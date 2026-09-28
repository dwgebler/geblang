package lower

import (
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/token"
	"geblang/internal/transpiler/types"
)

func isBareBuiltin(name string) bool {
	for _, n := range native.BareBuiltins {
		if n == name {
			return true
		}
	}
	return false
}

func (l *Lowerer) lowerBareBuiltin(id *ast.Identifier, e *ast.CallExpression) {
	if id.Value == "range" {
		if l.requirePositionalArgs(e.Arguments, id.Token, "range") {
			l.lowerRangeBuiltin(e.Arguments, id.Token)
		} else {
			l.w.WriteString("nil")
		}
		return
	}
	l.errAt(id.Token.Line, id.Token.Column,
		fmt.Sprintf("the transpiler does not yet support the %q builtin", id.Value),
		"this builtin needs runtime support deferred to a later phase")
	l.w.WriteString("nil")
}

func (l *Lowerer) lowerRangeBuiltin(args []ast.CallArgument, tok token.Token) {
	if len(args) < 2 || len(args) > 3 {
		l.errAt(tok.Line, tok.Column, "range expects (start, end) or (start, end, step)", "")
		l.w.WriteString("nil")
		return
	}
	l.Module.RequireHelper("gbRange")
	emitInt := func(expr ast.Expression) {
		l.w.WriteString("int64(")
		l.lowerExpression(expr)
		l.w.WriteString(")")
	}
	l.w.WriteString("gbRange(")
	emitInt(args[0].Value)
	l.w.WriteString(", ")
	emitInt(args[1].Value)
	l.w.WriteString(", ")
	if len(args) == 3 {
		emitInt(args[2].Value)
		l.w.WriteString(", false")
	} else {
		l.w.WriteString("0, true")
	}
	l.w.WriteString(")")
}

// builtinMethodKey identifies a lowering by receiver kind + method name.
type builtinMethodKey struct {
	kind types.Kind
	name string
}

// builtinMethodFn lowers one built-in method call; returns false to fall
// through (e.g. arity mismatch), matching the prior switch behaviour.
type builtinMethodFn func(l *Lowerer, sel *ast.SelectorExpression, ty *types.Type, args []ast.CallArgument) bool

func (l *Lowerer) tryLowerBuiltinMethod(sel *ast.SelectorExpression, ty *types.Type, args []ast.CallArgument) bool {
	if ty == nil {
		return false
	}
	fn, ok := builtinMethodTable[builtinMethodKey{ty.Kind, sel.Name.Value}]
	if !ok {
		return false
	}
	for _, a := range args {
		if a.Spread || a.Name != nil {
			l.errAt(sel.Token.Line, sel.Token.Column,
				"the transpiler does not yet support spread or named arguments on built-in methods",
				"")
			l.w.WriteString("nil")
			return true
		}
	}
	return fn(l, sel, ty, args)
}

// builtinMethodTable maps (receiver-kind, method) to its lowering. Keys are
// drawn from native.PrimitiveMethods; a guard test rejects phantom rows.
// Adding a supported method is a row here, not a switch arm.
var builtinMethodTable map[builtinMethodKey]builtinMethodFn

func init() { builtinMethodTable = buildBuiltinMethodTable() }

func buildBuiltinMethodTable() map[builtinMethodKey]builtinMethodFn {
	t := map[builtinMethodKey]builtinMethodFn{}
	for _, k := range []types.Kind{types.KindList, types.KindSet, types.KindBytes} {
		t[builtinMethodKey{k, "length"}] = lowerCollectionLength
		t[builtinMethodKey{k, "isEmpty"}] = lowerCollectionIsEmpty
	}
	t[builtinMethodKey{types.KindList, "contains"}] = lowerListContains
	for _, name := range []string{"push", "pop", "shift", "prepend", "unshift", "insert", "removeAt", "remove", "reverse", "takeFirst", "takeLast", "takeAt"} {
		t[builtinMethodKey{types.KindList, name}] = lowerListMutator
	}
	t[builtinMethodKey{types.KindList, "sort"}] = lowerListSort
	t[builtinMethodKey{types.KindList, "sortBy"}] = lowerListSortCmp
	t[builtinMethodKey{types.KindList, "sorted"}] = lowerListSorted
	t[builtinMethodKey{types.KindList, "reversed"}] = lowerListReversed
	t[builtinMethodKey{types.KindList, "join"}] = lowerListJoin
	t[builtinMethodKey{types.KindList, "map"}] = lowerListMap
	t[builtinMethodKey{types.KindList, "filter"}] = lowerListFilter
	t[builtinMethodKey{types.KindList, "reduce"}] = lowerListReduce
	t[builtinMethodKey{types.KindList, "find"}] = lowerListFind
	t[builtinMethodKey{types.KindList, "findLast"}] = lowerListFindLast
	t[builtinMethodKey{types.KindList, "any"}] = lowerListAny
	t[builtinMethodKey{types.KindList, "all"}] = lowerListAll
	t[builtinMethodKey{types.KindList, "count"}] = lowerListCount
	t[builtinMethodKey{types.KindList, "flatMap"}] = lowerListFlatMap
	// Dicts lower to transpilert.OrderedDict; methods route to its ordered API.
	t[builtinMethodKey{types.KindDict, "length"}] = lowerDictLength
	t[builtinMethodKey{types.KindDict, "isEmpty"}] = lowerDictIsEmpty
	for _, name := range []string{"hasKey", "contains"} {
		t[builtinMethodKey{types.KindDict, name}] = lowerDictHasKey
	}
	t[builtinMethodKey{types.KindDict, "keys"}] = lowerDictKeys
	t[builtinMethodKey{types.KindDict, "values"}] = lowerDictValues
	for _, name := range []string{"items", "entries"} {
		t[builtinMethodKey{types.KindDict, name}] = lowerDictItems
	}
	t[builtinMethodKey{types.KindDict, "get"}] = lowerDictGet
	for _, name := range []string{"set", "insert"} {
		t[builtinMethodKey{types.KindDict, name}] = lowerDictSet
	}
	for _, name := range []string{"delete", "remove"} {
		t[builtinMethodKey{types.KindDict, name}] = lowerDictDelete
	}

	t[builtinMethodKey{types.KindString, "length"}] = lowerStringLength
	t[builtinMethodKey{types.KindString, "isEmpty"}] = lowerStringIsEmpty
	for _, name := range []string{"contains", "startsWith", "endsWith"} {
		t[builtinMethodKey{types.KindString, name}] = lowerStringSearch
	}
	t[builtinMethodKey{types.KindString, "trim"}] = lowerStringTrim
	t[builtinMethodKey{types.KindString, "lower"}] = lowerStringCase
	t[builtinMethodKey{types.KindString, "upper"}] = lowerStringCase
	t[builtinMethodKey{types.KindString, "repeat"}] = lowerStringRepeat
	t[builtinMethodKey{types.KindString, "replace"}] = lowerStringReplace
	t[builtinMethodKey{types.KindString, "split"}] = lowerStringSplit
	t[builtinMethodKey{types.KindString, "indexOf"}] = lowerStringIndexOf
	registerStringRegexMethods(t)
	registerStringErgoMethods(t)
	// Grapheme segmentation is deferred in --native (kept out to stay pure
	// stdlib for vendoring); diagnose so it hard-fails to the VM, never miscompiles.
	for _, name := range []string{"graphemes", "graphemeLength", "truncateGraphemes"} {
		t[builtinMethodKey{types.KindString, name}] = lowerStringGraphemeDiagnose
	}
	registerBytesMethods(t)
	registerConversionMethods(t)
	t[builtinMethodKey{types.KindFloat, "isInt"}] = lowerNumericIsInt
	t[builtinMethodKey{types.KindDecimal, "isInt"}] = lowerNumericIsInt
	return t
}

// lowerNumericIsInt routes float.isInt/decimal.isInt to the transpilert
// whole-number predicates.
func lowerNumericIsInt(l *Lowerer, sel *ast.SelectorExpression, recv *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	helper := "FloatIsInt"
	if recv != nil && recv.Kind == types.KindDecimal {
		helper = "DecimalIsInt"
	}
	l.Module.AddImport(types.OrderedDictImport)
	l.w.WriteString("transpilert.")
	l.w.WriteString(helper)
	l.w.WriteString("(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(")")
	return true
}

// registerBytesMethods wires the bytes-value primitive methods to transpilert
// helpers (length/isEmpty handled by the collection loop). Receiver is []byte.
func registerBytesMethods(t map[builtinMethodKey]builtinMethodFn) {
	noArg := func(helper string) builtinMethodFn {
		return func(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
			if len(args) != 0 {
				return false
			}
			l.emitBytesHelper(helper, sel.Object)
			return true
		}
	}
	t[builtinMethodKey{types.KindBytes, "toHex"}] = noArg("BytesToHex")
	t[builtinMethodKey{types.KindBytes, "toBase64"}] = noArg("BytesToBase64")
	t[builtinMethodKey{types.KindBytes, "toBase64Url"}] = noArg("BytesToBase64Url")
	t[builtinMethodKey{types.KindBytes, "toList"}] = noArg("BytesToList")
	t[builtinMethodKey{types.KindBytes, "toString"}] = lowerBytesToString
	t[builtinMethodKey{types.KindBytes, "get"}] = lowerBytesGet
	t[builtinMethodKey{types.KindBytes, "contains"}] = lowerBytesContains
	t[builtinMethodKey{types.KindBytes, "slice"}] = lowerBytesSlice
}

func (l *Lowerer) emitBytesHelper(helper string, recv ast.Expression, args ...ast.Expression) {
	l.Module.AddImport(types.OrderedDictImport)
	l.w.WriteString("transpilert.")
	l.w.WriteString(helper)
	l.w.WriteString("(")
	l.lowerExpression(recv)
	for _, a := range args {
		l.w.WriteString(", ")
		l.lowerExpression(a)
	}
	l.w.WriteString(")")
}

// lowerBytesToString routes bytes.toString() to string(b); the optional utf-8
// encoding arg routes to BytesToStringEncoding.
func lowerBytesToString(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	switch len(args) {
	case 0:
		l.w.WriteString("string(")
		l.lowerExpression(sel.Object)
		l.w.WriteString(")")
		return true
	case 1:
		l.emitBytesHelper("BytesToStringEncoding", sel.Object, args[0].Value)
		return true
	}
	return false
}

func lowerBytesGet(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 1 {
		return false
	}
	l.Module.AddImport(types.OrderedDictImport)
	l.w.WriteString("transpilert.BytesGet(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(", int(")
	l.lowerExpression(args[0].Value)
	l.w.WriteString("))")
	return true
}

// lowerBytesContains passes the needle as any so the helper distinguishes a
// bytes sub-slice from a single int byte, matching the interpreter.
func lowerBytesContains(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 1 {
		return false
	}
	l.Module.AddImport(types.OrderedDictImport)
	l.w.WriteString("transpilert.BytesContains(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(", any(")
	l.lowerExpression(args[0].Value)
	l.w.WriteString("))")
	return true
}

func lowerBytesSlice(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) < 1 || len(args) > 2 {
		return false
	}
	l.Module.AddImport(types.OrderedDictImport)
	l.w.WriteString("transpilert.BytesSlice(")
	l.lowerExpression(sel.Object)
	if len(args) == 2 {
		l.w.WriteString(", true, int(")
		l.lowerExpression(args[0].Value)
		l.w.WriteString("), int(")
		l.lowerExpression(args[1].Value)
		l.w.WriteString("))")
	} else {
		l.w.WriteString(", false, int(")
		l.lowerExpression(args[0].Value)
		l.w.WriteString("), 0)")
	}
	return true
}

// registerConversionMethods wires toInt/toFloat/toDecimal/toBool/toString on
// each primitive kind to transpilert conversion helpers; fallible ones panic
// *transpilert.Error so the uncaught render and exit code match the interpreter.
func registerConversionMethods(t map[builtinMethodKey]builtinMethodFn) {
	for _, k := range []types.Kind{types.KindString, types.KindInt, types.KindFloat, types.KindDecimal, types.KindBool} {
		t[builtinMethodKey{k, "toInt"}] = lowerToInt
		t[builtinMethodKey{k, "toFloat"}] = lowerToFloat
		t[builtinMethodKey{k, "toDecimal"}] = lowerToDecimal
		t[builtinMethodKey{k, "toBool"}] = lowerToBool
		t[builtinMethodKey{k, "toString"}] = lowerToString
	}
}

func isPrimitiveKind(k types.Kind) bool {
	switch k {
	case types.KindString, types.KindInt, types.KindFloat, types.KindDecimal, types.KindBool, types.KindBytes:
		return true
	}
	return false
}

// isBuiltinReceiverKind reports a kind whose methods are lowered from the
// built-in method table; an unlowered method on one must diagnose, not emit
// raw Go.
func isBuiltinReceiverKind(k types.Kind) bool {
	if isPrimitiveKind(k) {
		return true
	}
	switch k {
	case types.KindList, types.KindDict, types.KindSet:
		return true
	}
	return false
}

func kindName(k types.Kind) string {
	switch k {
	case types.KindString:
		return "string"
	case types.KindInt:
		return "int"
	case types.KindFloat:
		return "float"
	case types.KindDecimal:
		return "decimal"
	case types.KindBool:
		return "bool"
	case types.KindBytes:
		return "bytes"
	case types.KindList:
		return "list"
	case types.KindDict:
		return "dict"
	case types.KindSet:
		return "set"
	}
	return "value"
}

func (l *Lowerer) emitConvert(goFn string, sel *ast.SelectorExpression) {
	l.Module.AddImport(types.OrderedDictImport)
	l.w.WriteString("transpilert.")
	l.w.WriteString(goFn)
	l.w.WriteString("(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(")")
}

func lowerToInt(l *Lowerer, sel *ast.SelectorExpression, ty *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	switch ty.Kind {
	case types.KindInt:
		l.lowerExpression(sel.Object)
	case types.KindString:
		l.requireUncaughtHandler()
		l.emitConvert("StringToInt", sel)
	case types.KindFloat:
		l.emitConvert("FloatToInt", sel)
	case types.KindDecimal:
		l.emitConvert("DecimalToInt", sel)
	default:
		return false
	}
	return true
}

func lowerToFloat(l *Lowerer, sel *ast.SelectorExpression, ty *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	switch ty.Kind {
	case types.KindFloat:
		l.lowerExpression(sel.Object)
	case types.KindString:
		l.requireUncaughtHandler()
		l.emitConvert("StringToFloat", sel)
	case types.KindInt:
		l.emitConvert("IntToFloat", sel)
	case types.KindDecimal:
		l.emitConvert("DecimalToFloat", sel)
	default:
		return false
	}
	return true
}

func lowerToDecimal(l *Lowerer, sel *ast.SelectorExpression, ty *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	switch ty.Kind {
	case types.KindDecimal:
		l.lowerExpression(sel.Object)
	case types.KindString:
		l.requireUncaughtHandler()
		l.emitConvert("StringToDecimal", sel)
	case types.KindInt:
		l.emitConvert("IntToDecimal", sel)
	case types.KindFloat:
		l.emitConvert("FloatToDecimal", sel)
	default:
		return false
	}
	return true
}

func lowerToBool(l *Lowerer, sel *ast.SelectorExpression, ty *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	switch ty.Kind {
	case types.KindBool:
		l.lowerExpression(sel.Object)
	case types.KindString:
		l.requireUncaughtHandler()
		l.emitConvert("StringToBool", sel)
	default:
		// int/float/decimal toBool are valid but not yet lowered: diagnose.
		return false
	}
	return true
}

func lowerToString(l *Lowerer, sel *ast.SelectorExpression, ty *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	switch ty.Kind {
	case types.KindString:
		l.lowerExpression(sel.Object)
	case types.KindInt:
		l.emitConvert("IntToString", sel)
	case types.KindFloat:
		l.emitConvert("FloatToString", sel)
	case types.KindDecimal:
		l.emitConvert("DecimalToString", sel)
	case types.KindBool:
		l.emitConvert("BoolToString", sel)
	default:
		return false
	}
	return true
}

func lowerStringGraphemeDiagnose(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, _ []ast.CallArgument) bool {
	l.errAt(sel.Token.Line, sel.Token.Column,
		fmt.Sprintf("the transpiler does not support grapheme method %q", sel.Name.Value),
		"grapheme segmentation is deferred; build with 'geblang build' for the bundled VM binary")
	l.w.WriteString("nil")
	return true
}

func lowerCollectionLength(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	l.w.WriteString("int64(len(")
	l.lowerExpression(sel.Object)
	l.w.WriteString("))")
	return true
}

func lowerCollectionIsEmpty(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	l.w.WriteString("(len(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(") == 0)")
	return true
}

func lowerListContains(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 1 {
		return false
	}
	l.Module.AddImport("slices")
	l.w.WriteString("slices.Contains(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(", ")
	l.lowerExpression(args[0].Value)
	l.w.WriteString(")")
	return true
}

func lowerDictLength(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	l.w.WriteString("int64(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(".Len())")
	return true
}

func lowerDictIsEmpty(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	l.w.WriteString("(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(".Len() == 0)")
	return true
}

func lowerDictHasKey(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 1 {
		return false
	}
	l.w.WriteString("func() bool { _, __ok := ")
	l.lowerExpression(sel.Object)
	l.w.WriteString(".Get(")
	l.lowerExpression(args[0].Value)
	l.w.WriteString("); return __ok }()")
	return true
}

func lowerDictKeys(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	l.lowerExpression(sel.Object)
	l.w.WriteString(".Keys()")
	return true
}

func lowerDictValues(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	l.lowerExpression(sel.Object)
	l.w.WriteString(".Values()")
	return true
}

// lowerDictItems yields insertion-ordered [key, value] pairs like the interpreter.
func lowerDictItems(l *Lowerer, sel *ast.SelectorExpression, ty *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	keyGo := types.ToGo(ty.Key, l.Module.IntMode)
	valGo := types.ToGo(ty.Value, l.Module.IntMode)
	l.w.WriteString("func() [][2]any { __out := [][2]any{}; ")
	l.lowerExpression(sel.Object)
	l.w.WriteString(".Entries(func(__k ")
	l.w.WriteString(keyGo.Source)
	l.w.WriteString(", __v ")
	l.w.WriteString(valGo.Source)
	l.w.WriteString(") bool { __out = append(__out, [2]any{__k, __v}); return true }); return __out }()")
	return true
}

// lowerDictGet returns the zero value of V on a miss, like the interpreter's null.
func lowerDictGet(l *Lowerer, sel *ast.SelectorExpression, ty *types.Type, args []ast.CallArgument) bool {
	if len(args) != 1 {
		return false
	}
	l.w.WriteString("func() ")
	valGo := types.ToGo(ty.Value, l.Module.IntMode)
	l.Module.AddTypeImports(valGo)
	l.w.WriteString(valGo.Source)
	l.w.WriteString(" { __v, _ := ")
	l.lowerExpression(sel.Object)
	l.w.WriteString(".Get(")
	l.lowerExpression(args[0].Value)
	l.w.WriteString("); return __v }()")
	return true
}

func lowerDictSet(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 2 {
		return false
	}
	l.lowerExpression(sel.Object)
	l.w.WriteString(".Set(")
	l.lowerExpression(args[0].Value)
	l.w.WriteString(", ")
	l.lowerExpression(args[1].Value)
	l.w.WriteString(")")
	return true
}

func lowerDictDelete(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 1 {
		return false
	}
	l.lowerExpression(sel.Object)
	l.w.WriteString(".Delete(")
	l.lowerExpression(args[0].Value)
	l.w.WriteString(")")
	return true
}

func lowerStringLength(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	l.w.WriteString("int64(len([]rune(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(")))")
	return true
}

func lowerStringIsEmpty(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	l.w.WriteString("(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(` == "")`)
	return true
}

func lowerStringSearch(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 1 {
		return false
	}
	l.Module.AddImport("strings")
	goFn := map[string]string{
		"contains":   "strings.Contains",
		"startsWith": "strings.HasPrefix",
		"endsWith":   "strings.HasSuffix",
	}[sel.Name.Value]
	l.w.WriteString(goFn)
	l.w.WriteString("(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(", ")
	l.lowerExpression(args[0].Value)
	l.w.WriteString(")")
	return true
}

func lowerStringTrim(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	l.Module.AddImport("strings")
	l.w.WriteString("strings.TrimSpace(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(")")
	return true
}

func lowerStringCase(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 0 {
		return false
	}
	l.Module.AddImport("strings")
	fn := "strings.ToLower"
	if sel.Name.Value == "upper" {
		fn = "strings.ToUpper"
	}
	l.w.WriteString(fn)
	l.w.WriteString("(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(")")
	return true
}

func lowerStringRepeat(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 1 {
		return false
	}
	l.Module.AddImport("strings")
	l.w.WriteString("strings.Repeat(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(", int(")
	l.lowerExpression(args[0].Value)
	l.w.WriteString("))")
	return true
}

func lowerStringReplace(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) < 2 || len(args) > 3 {
		return false
	}
	l.Module.AddImport("strings")
	l.w.WriteString("strings.Replace(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(", ")
	l.lowerExpression(args[0].Value)
	l.w.WriteString(", ")
	l.lowerExpression(args[1].Value)
	l.w.WriteString(", ")
	if len(args) == 3 {
		l.w.WriteString("int(")
		l.lowerExpression(args[2].Value)
		l.w.WriteString(")")
	} else {
		l.w.WriteString("-1")
	}
	l.w.WriteString(")")
	return true
}

func lowerStringSplit(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 1 {
		return false
	}
	l.Module.AddImport("strings")
	l.w.WriteString("strings.Split(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(", ")
	l.lowerExpression(args[0].Value)
	l.w.WriteString(")")
	return true
}

func lowerStringIndexOf(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) != 1 {
		return false
	}
	l.Module.AddImport("strings")
	l.w.WriteString("int64(strings.Index(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(", ")
	l.lowerExpression(args[0].Value)
	l.w.WriteString("))")
	return true
}

// registerStringErgoMethods wires the string ergonomics + slicing methods that
// route to transpilert helpers whose behaviour matches the native dispatch
// byte-for-byte (reverse/chars/codePoints/slice/substring/pad/capitalize/title/
// isBlank/lines/removePrefix/removeSuffix/equalsIgnoreCase/containsIgnoreCase/
// lastIndexOf/count, plus unicode-correct trimStart/trimEnd).
func registerStringErgoMethods(t map[builtinMethodKey]builtinMethodFn) {
	noArg := func(helper string) builtinMethodFn {
		return func(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
			if len(args) != 0 {
				return false
			}
			l.emitStringHelper(helper, sel.Object)
			return true
		}
	}
	oneStr := func(helper string) builtinMethodFn {
		return func(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
			if len(args) != 1 {
				return false
			}
			l.emitStringHelper(helper, sel.Object, args[0].Value)
			return true
		}
	}
	t[builtinMethodKey{types.KindString, "trimStart"}] = noArg("StringTrimStart")
	t[builtinMethodKey{types.KindString, "trimEnd"}] = noArg("StringTrimEnd")
	t[builtinMethodKey{types.KindString, "reverse"}] = noArg("StringReverse")
	t[builtinMethodKey{types.KindString, "chars"}] = noArg("StringChars")
	t[builtinMethodKey{types.KindString, "codePoints"}] = noArg("StringCodePoints")
	t[builtinMethodKey{types.KindString, "capitalize"}] = noArg("StringCapitalize")
	t[builtinMethodKey{types.KindString, "title"}] = noArg("StringTitle")
	t[builtinMethodKey{types.KindString, "isBlank"}] = noArg("StringIsBlank")
	t[builtinMethodKey{types.KindString, "isInt"}] = noArg("StringIsInt")
	t[builtinMethodKey{types.KindString, "isDecimal"}] = noArg("StringIsDecimal")
	t[builtinMethodKey{types.KindString, "isNumeric"}] = noArg("StringIsNumeric")
	t[builtinMethodKey{types.KindString, "lines"}] = noArg("StringLines")
	t[builtinMethodKey{types.KindString, "removePrefix"}] = oneStr("StringRemovePrefix")
	t[builtinMethodKey{types.KindString, "removeSuffix"}] = oneStr("StringRemoveSuffix")
	t[builtinMethodKey{types.KindString, "equalsIgnoreCase"}] = oneStr("StringEqualsIgnoreCase")
	t[builtinMethodKey{types.KindString, "containsIgnoreCase"}] = oneStr("StringContainsIgnoreCase")
	t[builtinMethodKey{types.KindString, "count"}] = oneStr("StringCount")
	t[builtinMethodKey{types.KindString, "lastIndexOf"}] = oneStr("StringLastIndexOf")
	for _, name := range []string{"slice", "substring"} {
		t[builtinMethodKey{types.KindString, name}] = lowerStringSlice
	}
	for _, name := range []string{"padStart", "padEnd"} {
		t[builtinMethodKey{types.KindString, name}] = lowerStringPad
	}
}

// RePatternType is the type of re.compile's result: an opaque class type whose
// chained methods route to transpilert.RePattern via lowerRePatternMethod.
func RePatternType() *types.Type {
	return &types.Type{Kind: types.KindClass, Name: types.RePatternName}
}

// rePatternMethodReturnType pins the result type of each RePattern method so a
// let-bound result routes later dispatch (e.g. p.split(t).length()).
func rePatternMethodReturnType(method string) *types.Type {
	switch method {
	case "test":
		return &types.Type{Kind: types.KindBool}
	case "find":
		return &types.Type{Kind: types.KindString, Nullable: true}
	case "replace":
		return &types.Type{Kind: types.KindString}
	case "findAll", "split":
		return &types.Type{Kind: types.KindList, Elem: &types.Type{Kind: types.KindString}}
	case "match":
		return &types.Type{Kind: types.KindDict, Key: &types.Type{Kind: types.KindString}, Value: &types.Type{Kind: types.KindAny}, Nullable: true}
	case "matchAll":
		return &types.Type{Kind: types.KindList, Elem: &types.Type{Kind: types.KindDict, Key: &types.Type{Kind: types.KindString}, Value: &types.Type{Kind: types.KindAny}}}
	}
	return nil
}

// lowerRePatternMethod routes a chained call on a re.compile result to the
// matching transpilert.RePattern method (Go-cased), arity-checked.
func (l *Lowerer) lowerRePatternMethod(sel *ast.SelectorExpression, args []ast.CallArgument) bool {
	want := map[string]int{"test": 1, "find": 1, "findAll": 1, "match": 1, "matchAll": 1, "split": 1, "replace": 2}
	goName := map[string]string{
		"test": "Test", "find": "Find", "findAll": "FindAll", "match": "Match",
		"matchAll": "MatchAll", "split": "Split", "replace": "Replace",
	}
	n, ok := want[sel.Name.Value]
	if !ok {
		l.errAt(sel.Token.Line, sel.Token.Column,
			fmt.Sprintf("the transpiler does not support re.Pattern method %q", sel.Name.Value),
			"build with 'geblang build' for the bundled VM binary")
		l.w.WriteString("nil")
		return true
	}
	if len(args) != n {
		l.errAt(sel.Token.Line, sel.Token.Column,
			fmt.Sprintf("re.Pattern.%s expects %d argument(s)", sel.Name.Value, n), "")
		l.w.WriteString("nil")
		return true
	}
	l.lowerExpression(sel.Object)
	l.w.WriteString(".")
	l.w.WriteString(goName[sel.Name.Value])
	l.emitPositionalArgs(args)
	return true
}

// URLValueType is the type of url.URL's result: an opaque class type whose
// chained methods route to transpilert.URLValue via lowerURLValueMethod.
func URLValueType() *types.Type {
	return &types.Type{Kind: types.KindClass, Name: types.URLValueName}
}

// urlValueMethodReturnType pins each URLValue method's result so a let-bound
// result routes later dispatch; with*/resolve/normalize return the handle.
func urlValueMethodReturnType(method string) *types.Type {
	str := func() *types.Type { return &types.Type{Kind: types.KindString} }
	anyDict := func() *types.Type {
		return &types.Type{Kind: types.KindDict, Key: str(), Value: &types.Type{Kind: types.KindAny}}
	}
	switch method {
	case "toString", "scheme", "host", "port", "path", "fragment":
		return str()
	case "toDict", "query":
		return anyDict()
	case "withScheme", "withHost", "withPath", "withQuery", "withFragment", "resolve", "normalize":
		return URLValueType()
	}
	return nil
}

// lowerURLValueMethod routes a chained call on a url.URL result to the matching
// transpilert.URLValue method (Go-cased), arity-checked.
func (l *Lowerer) lowerURLValueMethod(sel *ast.SelectorExpression, args []ast.CallArgument) bool {
	want := map[string]int{
		"toString": 0, "toDict": 0, "scheme": 0, "host": 0, "port": 0, "path": 0,
		"query": 0, "fragment": 0, "normalize": 0,
		"withScheme": 1, "withHost": 1, "withPath": 1, "withQuery": 1, "withFragment": 1, "resolve": 1,
	}
	goName := map[string]string{
		"toString": "ToString", "toDict": "ToDict", "scheme": "Scheme", "host": "Host",
		"port": "Port", "path": "Path", "query": "Query", "fragment": "Fragment",
		"normalize": "Normalize", "withScheme": "WithScheme", "withHost": "WithHost",
		"withPath": "WithPath", "withQuery": "WithQuery", "withFragment": "WithFragment", "resolve": "Resolve",
	}
	n, ok := want[sel.Name.Value]
	if !ok {
		l.errAt(sel.Token.Line, sel.Token.Column,
			fmt.Sprintf("the transpiler does not support url.URL method %q", sel.Name.Value),
			"build with 'geblang build' for the bundled VM binary")
		l.w.WriteString("nil")
		return true
	}
	if len(args) != n {
		l.errAt(sel.Token.Line, sel.Token.Column,
			fmt.Sprintf("url.URL.%s expects %d argument(s)", sel.Name.Value, n), "")
		l.w.WriteString("nil")
		return true
	}
	l.lowerExpression(sel.Object)
	l.w.WriteString(".")
	l.w.WriteString(goName[sel.Name.Value])
	l.emitPositionalArgs(args)
	return true
}

// DateTimeInstantType / DateTimeDurationType / DateTimeZoneType are the opaque
// class types for the datetime OO handles; chained methods route via the
// matching lowerDateTime*Method.
func DateTimeInstantType() *types.Type {
	return &types.Type{Kind: types.KindClass, Name: types.DateTimeInstantName}
}
func DateTimeDurationType() *types.Type {
	return &types.Type{Kind: types.KindClass, Name: types.DateTimeDurationName}
}
func DateTimeZoneType() *types.Type {
	return &types.Type{Kind: types.KindClass, Name: types.DateTimeZoneName}
}

func dtIntT() *types.Type  { return &types.Type{Kind: types.KindInt} }
func dtStrT() *types.Type  { return &types.Type{Kind: types.KindString} }
func dtBoolT() *types.Type { return &types.Type{Kind: types.KindBool} }
func dtAnyDictT() *types.Type {
	return &types.Type{Kind: types.KindDict, Key: dtStrT(), Value: &types.Type{Kind: types.KindAny}}
}
func dtIntDictT() *types.Type {
	return &types.Type{Kind: types.KindDict, Key: dtStrT(), Value: dtIntT()}
}

// dateTimeInstantMethodReturnType pins each Instant method's result so a
// let-bound result routes later dispatch; add*/sub/copy return an Instant, diff
// returns a Duration, inZone/parts a dict, comparisons a bool.
func dateTimeInstantMethodReturnType(method string) *types.Type {
	switch method {
	case "copy", "add", "sub", "addSeconds", "addDays", "addMonths", "addYears":
		return DateTimeInstantType()
	case "diff":
		return DateTimeDurationType()
	case "unix", "toUnix", "toUnixMillis", "toUnixNanos",
		"year", "month", "day", "hour", "minute", "second", "weekday", "dayOfYear":
		return dtIntT()
	case "toString", "formatRFC3339", "toUtc", "formatHTTP", "format", "toLocal":
		return dtStrT()
	case "isBefore", "isAfter", "equals", "isWeekend":
		return dtBoolT()
	case "inZone", "parts":
		return dtAnyDictT()
	}
	return nil
}

func dateTimeDurationMethodReturnType(method string) *types.Type {
	switch method {
	case "abs", "negate", "add", "sub":
		return DateTimeDurationType()
	case "seconds", "inSeconds", "inMillis", "inNanos":
		return dtIntT()
	case "toDict":
		return dtIntDictT()
	case "toString":
		return dtStrT()
	}
	return nil
}

func dateTimeZoneMethodReturnType(method string) *types.Type {
	switch method {
	case "name", "toString":
		return dtStrT()
	case "offset", "offsetAt":
		return dtIntT()
	}
	return nil
}

// lowerDateTimeInstantMethod routes a chained call on a datetime.Instant handle
// to the matching transpilert.DateTimeInstant method (Go-cased), arity-checked.
func (l *Lowerer) lowerDateTimeInstantMethod(sel *ast.SelectorExpression, args []ast.CallArgument) bool {
	want := map[string]int{
		"copy": 0, "unix": 0, "toUnix": 0, "toUnixMillis": 0, "toUnixNanos": 0,
		"toString": 0, "formatRFC3339": 0, "toUtc": 0, "formatHTTP": 0, "parts": 0,
		"year": 0, "month": 0, "day": 0, "hour": 0, "minute": 0, "second": 0,
		"weekday": 0, "dayOfYear": 0, "isWeekend": 0,
		"format": 1, "toLocal": 1, "add": 1, "sub": 1, "addSeconds": 1,
		"addDays": 1, "addMonths": 1, "addYears": 1, "diff": 1,
		"isBefore": 1, "isAfter": 1, "equals": 1, "inZone": 1,
	}
	goName := map[string]string{
		"copy": "Copy", "unix": "Unix_", "toUnix": "Unix_", "toUnixMillis": "ToUnixMillis",
		"toUnixNanos": "ToUnixNanos", "toString": "ToString", "formatRFC3339": "ToString",
		"toUtc": "ToString", "formatHTTP": "FormatHTTP", "format": "Format", "toLocal": "ToLocal",
		"add": "Add", "sub": "Sub", "addSeconds": "AddSeconds", "addDays": "AddDays",
		"addMonths": "AddMonths", "addYears": "AddYears", "diff": "Diff", "inZone": "InZone",
		"parts": "Parts", "isBefore": "IsBefore", "isAfter": "IsAfter", "equals": "Equals",
		"year": "Year", "month": "Month", "day": "Day", "hour": "Hour", "minute": "Minute",
		"second": "Second", "weekday": "Weekday", "dayOfYear": "DayOfYear", "isWeekend": "IsWeekend",
	}
	return l.lowerHandleMethod("datetime.Instant", want, goName, sel, args)
}

func (l *Lowerer) lowerDateTimeDurationMethod(sel *ast.SelectorExpression, args []ast.CallArgument) bool {
	want := map[string]int{
		"seconds": 0, "inSeconds": 0, "inMillis": 0, "inNanos": 0, "abs": 0, "negate": 0,
		"toDict": 0, "toString": 0, "add": 1, "sub": 1,
	}
	goName := map[string]string{
		"seconds": "Seconds_", "inSeconds": "Seconds_", "inMillis": "InMillis", "inNanos": "InNanos",
		"abs": "Abs", "negate": "Negate", "toDict": "ToDict", "toString": "ToString",
		"add": "Add", "sub": "Sub",
	}
	return l.lowerHandleMethod("datetime.Duration", want, goName, sel, args)
}

func (l *Lowerer) lowerDateTimeZoneMethod(sel *ast.SelectorExpression, args []ast.CallArgument) bool {
	want := map[string]int{"name": 0, "toString": 0, "offset": 0, "offsetAt": 1}
	goName := map[string]string{"name": "Name_", "toString": "ToString", "offset": "Offset", "offsetAt": "OffsetAt"}
	return l.lowerHandleMethod("datetime.Zone", want, goName, sel, args)
}

// lowerHandleMethod is the shared body for the three datetime handle method
// routers: arity-check, then emit recv.GoName(args).
func (l *Lowerer) lowerHandleMethod(handle string, want map[string]int, goName map[string]string, sel *ast.SelectorExpression, args []ast.CallArgument) bool {
	n, ok := want[sel.Name.Value]
	if !ok {
		l.errAt(sel.Token.Line, sel.Token.Column,
			fmt.Sprintf("the transpiler does not support %s method %q", handle, sel.Name.Value),
			"build with 'geblang build' for the bundled VM binary")
		l.w.WriteString("nil")
		return true
	}
	if len(args) != n {
		l.errAt(sel.Token.Line, sel.Token.Column,
			fmt.Sprintf("%s.%s expects %d argument(s)", handle, sel.Name.Value, n), "")
		l.w.WriteString("nil")
		return true
	}
	l.lowerExpression(sel.Object)
	l.w.WriteString(".")
	l.w.WriteString(goName[sel.Name.Value])
	l.emitPositionalArgs(args)
	return true
}

// TemplateValueType / TemplateEngineType are the opaque class types for the
// template handles; chained methods route via lowerTemplate*Method.
func TemplateValueType() *types.Type {
	return &types.Type{Kind: types.KindClass, Name: types.TemplateValueName}
}
func TemplateEngineType() *types.Type {
	return &types.Type{Kind: types.KindClass, Name: types.TemplateEngineName}
}

// templateValueMethodReturnType pins each Template method's result so a
// let-bound result routes later dispatch.
func templateValueMethodReturnType(method string) *types.Type {
	switch method {
	case "name", "render", "toString":
		return &types.Type{Kind: types.KindString}
	case "path":
		return &types.Type{Kind: types.KindString, Nullable: true}
	}
	return nil
}

func templateEngineMethodReturnType(method string) *types.Type {
	switch method {
	case "dir", "render":
		return &types.Type{Kind: types.KindString}
	case "load":
		return TemplateValueType()
	}
	return nil
}

func (l *Lowerer) lowerTemplateValueMethod(sel *ast.SelectorExpression, args []ast.CallArgument) bool {
	want := map[string]int{"name": 0, "path": 0, "toString": 0, "render": 1}
	goName := map[string]string{"name": "Name_", "path": "PathOrNull", "toString": "ToString", "render": "Render"}
	return l.lowerHandleMethod("template.Template", want, goName, sel, args)
}

func (l *Lowerer) lowerTemplateEngineMethod(sel *ast.SelectorExpression, args []ast.CallArgument) bool {
	want := map[string]int{"dir": 0, "load": 1, "render": 2}
	goName := map[string]string{"dir": "Dir_", "load": "Load", "render": "Render"}
	return l.lowerHandleMethod("template.Engine", want, goName, sel, args)
}

// registerStringRegexMethods wires the RE2-backed string regex methods to
// transpilert helpers; the receiver is the text, args are pattern (+replacement).
func registerStringRegexMethods(t map[builtinMethodKey]builtinMethodFn) {
	t[builtinMethodKey{types.KindString, "matchesRegex"}] = func(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
		if len(args) != 1 {
			return false
		}
		l.emitStringHelper("StringMatchesRegex", sel.Object, args[0].Value)
		return true
	}
	t[builtinMethodKey{types.KindString, "splitRegex"}] = func(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
		if len(args) != 1 {
			return false
		}
		l.emitStringHelper("StringSplitRegex", sel.Object, args[0].Value)
		return true
	}
	t[builtinMethodKey{types.KindString, "replaceRegex"}] = func(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
		if len(args) != 2 {
			return false
		}
		l.emitStringHelper("StringReplaceRegex", sel.Object, args[0].Value, args[1].Value)
		return true
	}
}

// emitStringHelper writes transpilert.<helper>(recv[, args...]) with int args
// coerced to Go int (helper signatures take int, the interpreter uses code-point
// counts).
func (l *Lowerer) emitStringHelper(helper string, recv ast.Expression, args ...ast.Expression) {
	l.Module.AddImport(types.OrderedDictImport)
	l.w.WriteString("transpilert.")
	l.w.WriteString(helper)
	l.w.WriteString("(")
	l.lowerExpression(recv)
	for _, a := range args {
		l.w.WriteString(", ")
		l.lowerExpression(a)
	}
	l.w.WriteString(")")
}

// lowerStringSlice handles slice/substring with one or two int code-point
// indices; StringSliceFrom covers the start-only form.
func lowerStringSlice(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) < 1 || len(args) > 2 {
		return false
	}
	l.Module.AddImport(types.OrderedDictImport)
	if len(args) == 1 {
		l.w.WriteString("transpilert.StringSliceFrom(")
		l.lowerExpression(sel.Object)
		l.w.WriteString(", int(")
		l.lowerExpression(args[0].Value)
		l.w.WriteString("))")
		return true
	}
	l.w.WriteString("transpilert.StringSlice(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(", int(")
	l.lowerExpression(args[0].Value)
	l.w.WriteString("), int(")
	l.lowerExpression(args[1].Value)
	l.w.WriteString("))")
	return true
}

// lowerStringPad handles padStart/padEnd; the optional pad defaults to a space,
// matching the interpreter.
func lowerStringPad(l *Lowerer, sel *ast.SelectorExpression, _ *types.Type, args []ast.CallArgument) bool {
	if len(args) < 1 || len(args) > 2 {
		return false
	}
	l.Module.AddImport(types.OrderedDictImport)
	helper := "StringPadStart"
	if sel.Name.Value == "padEnd" {
		helper = "StringPadEnd"
	}
	l.w.WriteString("transpilert.")
	l.w.WriteString(helper)
	l.w.WriteString("(")
	l.lowerExpression(sel.Object)
	l.w.WriteString(", int(")
	l.lowerExpression(args[0].Value)
	l.w.WriteString("), ")
	if len(args) == 2 {
		l.lowerExpression(args[1].Value)
	} else {
		l.w.WriteString(`" "`)
	}
	l.w.WriteString(")")
	return true
}

func builtinMethodReturnType(method string, recv *types.Type) *types.Type {
	if recv == nil {
		return nil
	}
	if isPrimitiveKind(recv.Kind) {
		switch method {
		case "toInt":
			return &types.Type{Kind: types.KindInt}
		case "toFloat":
			return &types.Type{Kind: types.KindFloat}
		case "toDecimal":
			return &types.Type{Kind: types.KindDecimal}
		case "toBool":
			return &types.Type{Kind: types.KindBool}
		case "toString":
			return &types.Type{Kind: types.KindString}
		}
	}
	switch recv.Kind {
	case types.KindString:
		switch method {
		case "length", "indexOf", "lastIndexOf", "count":
			return &types.Type{Kind: types.KindInt}
		case "isEmpty", "contains", "startsWith", "endsWith", "isBlank", "equalsIgnoreCase", "containsIgnoreCase", "matchesRegex",
			"isInt", "isDecimal", "isNumeric":
			return &types.Type{Kind: types.KindBool}
		case "lower", "upper", "trim", "trimStart", "trimEnd", "repeat", "replace",
			"reverse", "slice", "substring", "padStart", "padEnd",
			"capitalize", "title", "removePrefix", "removeSuffix", "replaceRegex":
			return &types.Type{Kind: types.KindString}
		case "split", "chars", "lines", "splitRegex":
			return &types.Type{Kind: types.KindList, Elem: &types.Type{Kind: types.KindString}}
		case "codePoints":
			return &types.Type{Kind: types.KindList, Elem: &types.Type{Kind: types.KindInt}}
		}
	case types.KindFloat, types.KindDecimal:
		if method == "isInt" {
			return &types.Type{Kind: types.KindBool}
		}
	case types.KindList:
		switch method {
		case "length", "count":
			return &types.Type{Kind: types.KindInt}
		case "isEmpty", "contains", "any", "all":
			return &types.Type{Kind: types.KindBool}
		case "push", "pop", "shift", "prepend", "unshift", "insert", "removeAt", "remove", "reverse", "sort", "sortBy", "sorted", "reversed":
			return recv
		case "takeFirst", "takeLast", "takeAt":
			return recv.Elem
		case "join":
			return &types.Type{Kind: types.KindString}
		case "find", "findLast":
			return &types.Type{Kind: types.KindAny}
		case "filter":
			return recv
		}
	case types.KindDict:
		switch method {
		case "length":
			return &types.Type{Kind: types.KindInt}
		case "isEmpty":
			return &types.Type{Kind: types.KindBool}
		case "hasKey", "contains":
			return &types.Type{Kind: types.KindBool}
		case "keys":
			return &types.Type{Kind: types.KindList, Elem: recv.Key}
		case "values":
			return &types.Type{Kind: types.KindList, Elem: recv.Value}
		case "get":
			return recv.Value
		}
	case types.KindSet:
		if method == "length" {
			return &types.Type{Kind: types.KindInt}
		}
	case types.KindBytes:
		switch method {
		case "length", "get":
			return &types.Type{Kind: types.KindInt}
		case "isEmpty", "contains":
			return &types.Type{Kind: types.KindBool}
		case "toHex", "toBase64", "toBase64Url", "toString":
			return &types.Type{Kind: types.KindString}
		case "toList":
			return &types.Type{Kind: types.KindList, Elem: &types.Type{Kind: types.KindInt}}
		case "slice":
			return &types.Type{Kind: types.KindBytes}
		}
	}
	return nil
}

// hofMethodReturnType infers list HOF results that depend on the closure
// argument, so a let-bound HOF result resolves to a list for later dispatch.
func (l *Lowerer) hofMethodReturnType(method string, recv *types.Type, args []ast.CallArgument) *types.Type {
	if recv == nil || recv.Kind != types.KindList {
		return nil
	}
	switch method {
	case "map":
		if len(args) == 1 {
			return &types.Type{Kind: types.KindList, Elem: l.closureReturnType(args[0].Value)}
		}
	case "flatMap":
		if len(args) == 1 {
			if elemList := l.closureReturnType(args[0].Value); elemList != nil && elemList.Kind == types.KindList {
				return &types.Type{Kind: types.KindList, Elem: elemList.Elem}
			}
			return &types.Type{Kind: types.KindList, Elem: types.Any()}
		}
	case "reduce":
		if len(args) == 2 {
			if acc := l.inferExpressionType(args[1].Value); acc != nil && acc.Kind != types.KindUnknown {
				return acc
			}
			return l.closureReturnType(args[0].Value)
		}
	}
	return nil
}

func (l *Lowerer) closureReturnType(arg ast.Expression) *types.Type {
	if fn, ok := arg.(*ast.FunctionLiteral); ok && fn.ReturnType != nil {
		if t := l.resolveTypeRef(fn.ReturnType); t != nil {
			return t
		}
	}
	return types.Any()
}
