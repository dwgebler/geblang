package bytecode

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"geblang/internal/native"
	"geblang/internal/runtime"
)

const (
	Magic   = "GEBBC"
	Version = uint16(78)
)

// Spread-call per-argument metadata: a named arg carries its name constant index, others use these sentinels.
const (
	spreadArgMeta     int64 = -2
	positionalArgMeta int64 = -1
)

type Op byte

const (
	OpNoop Op = iota
	OpConstant
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpPop
	OpDup
	OpDefineGlobal
	OpGetGlobal
	OpSetGlobal
	OpPrintln
	OpPrint
	OpEqual
	OpLess
	OpGreater
	OpLessEqual
	OpGreaterEqual
	OpJump
	OpJumpIfFalse
	OpReturn
	OpNot
	OpNegate
	OpBoolXor
	OpIntDiv
	OpMod
	OpPow
	OpDefineLocal
	OpGetLocal
	OpSetLocal
	OpBuildList
	OpBuildDict
	OpBuildSet
	OpIndex
	OpSetIndex
	OpSlice
	OpIterInit
	OpIterNext
	OpUnpackList
	OpBuildRange
	OpExit
	OpCall
	OpTypeOf
	OpInstanceOf
	OpCast
	OpMethodCall
	OpRuntimeError
	OpDeferPrint
	OpDeferPrintln
	OpDeferNativeCall
	OpDeferFuncCall
	OpDeferMethodCall
	OpDeferCallableCall
	OpNativeCall
	OpDefineClass
	OpConstructClass
	OpGetField
	OpSetField
	OpCallParentConstructor
	OpCallParentMethod
	OpGetStaticValue
	OpSetStaticValue
	OpCallStaticMethod
	OpIdentical
	OpMakeError
	OpPushExceptionHandler
	OpPopExceptionHandler
	OpThrow
	OpCatch
	OpRethrow
	OpMethodCallNamed
	OpImportModule
	// Loads the module value named by Operand[0] so reflect lookups resolve over native modules.
	OpLoadModuleValue
	OpMakeClosure
	OpBitAnd
	OpBitOr
	OpBitXor
	OpBitNot
	OpLShift
	OpRShift
	OpNullCoalesce
	OpOptionalChain
	OpCallSpread
	OpListConcat
	OpNativeCallNamed
	OpAwait
	OpMatchError
	OpSetTypeBindings
	OpYield
	OpIterClose
	OpTypeAssert
	OpShallowFreeze
	// OpMatchListShape pops the top-of-stack value and pushes a Bool:
	// true when the value is a *runtime.List with exactly Operand[0]
	// elements; false otherwise. Used by match list-pattern compile
	// (case [a, b, ...]) so the structural test stays a single
	// dispatch.
	OpMatchListShape
	// Type-specialized integer arithmetic and comparison opcodes.
	// These are emitted by the compiler when both operands are statically typed
	// as int. The VM fast-path skips type dispatch and handles SmallInt inline.
	OpAddInt
	OpSubInt
	OpMulInt
	OpModInt
	OpLessInt
	OpGreaterInt
	OpLessEqualInt
	OpGreaterEqualInt
	OpEqualInt
	OpIncLocalInt
	OpDecLocalInt
	OpIncGlobalInt
	OpDecGlobalInt
	OpAppendLocalList
	OpAppendGlobalList
	// Phase 11: fused integer compare-and-branch opcodes. Each takes a single
	// jump-target operand and pops two int values from the stack. The opcode
	// jumps to the target when the "skip body" condition holds (i.e., the
	// underlying boolean condition is false).
	OpJumpIfNotLessInt
	OpJumpIfNotLessEqualInt
	OpJumpIfNotGreaterInt
	OpJumpIfNotGreaterEqualInt
	OpJumpIfNotEqualInt
	OpJumpIfEqualInt
	// Fused `if (local % const_divisor == 0)` / `!= 0` superinstructions.
	// Operands are [target, localSlot, divisor]. The op reads the local
	// directly, computes the modulo against the divisor constant, and
	// jumps to target when the named condition holds (skip body). Saves
	// 4 dispatches per iteration vs the GetLocal/Const/Mod/Const/JumpIfX
	// sequence on the bench numeric_loop hot path.
	OpJumpIfModNotZero
	OpJumpIfModZero
	// Phase 12: fused local/global int self-update arithmetic opcodes. Each
	// stores back into the destination slot and pushes the result onto the
	// stack (matching OpSetLocal / OpAppendLocalList semantics so the
	// surrounding ExpressionStatement OpPop discards it).
	OpAddLocalIntLocal
	OpSubLocalIntLocal
	OpAddLocalIntConst
	OpSubLocalIntConst
	OpAddGlobalIntGlobal
	OpSubGlobalIntGlobal
	OpAddGlobalIntConst
	OpSubGlobalIntConst
	// Cross-scope variants for the common module-level-accumulator pattern
	// (`total` is a global, `i` is a for-loop local).
	OpAddGlobalIntLocal
	OpSubGlobalIntLocal
	OpAddLocalIntGlobal
	OpSubLocalIntGlobal
	// Spread-aware method/__invoke call for value callables (closures, locals,
	// instances) where the function index isn't known at compile time. Operands
	// are [methodNameIndex, staticArgCount]; stack layout is
	// [receiver, static_args..., spread_list_or_dict].
	OpMethodCallSpread
	// OpWithEnter takes [hiddenSlot] as the operand: reads the
	// resource value from hiddenSlot, calls __enter__() if the
	// class defines one (otherwise leaves the resource itself),
	// and pushes the result onto the stack.
	OpWithEnter
	// OpWithExit takes [hiddenSlot] as the operand: reads the
	// resource value from hiddenSlot and calls __exit__() if the
	// class defines one, else the class destructor if one exists,
	// else does nothing. Pops nothing, pushes nothing.
	OpWithExit
	// OpDel implements `del x`. Operands are [slot, kind] where
	// kind==0 means local and kind==1 means global. If the value
	// at the slot is a class instance whose class declares
	// `~ClassName()` and hasn't already been destroyed, the VM
	// fires the destructor, unregisters the instance from the
	// destructible-instance list, and writes Null{} back to the
	// slot. Use-after-del is rejected by the semantic analyzer;
	// the runtime check exists only as a defensive backstop.
	OpDel
	// OpPlantCallTypeBindings is emitted immediately before OpCall
	// when a generic function is called with an explicit
	// `<TypeArgs>` clause. Operands are [count, paramName1Idx,
	// typeName1Idx, paramName2Idx, typeName2Idx, ...] - constant
	// indices into the chunk's string pool. The handler merges
	// these bindings into `vm.pendingTypeBindings`; the following
	// OpCall drains pendingTypeBindings into the new frame's
	// typeBindings (reusing the closure-inheritance path). The
	// per-call inference loop then skips type-parameter names
	// already bound, making explicit-args strictly override
	// inferred-from-arg bindings.
	OpPlantCallTypeBindings
	// OpPlantCallTypeArgs stages a selector call's explicit `<TypeArgs>`
	// positionally ([count, typeName1Idx, ...]): the calling chunk cannot
	// know the remote callee's type-parameter names, so the dispatch site
	// zips them against the callee's declared params in its home chunk.
	OpPlantCallTypeArgs
	// OpRange implements the top-level `range(start, end[, step])`
	// builtin. The single operand is the argument count (2 or 3).
	// Pops that many integer-valued operands from the stack and
	// pushes a `list<int>` containing the inclusive sequence. With
	// 2 args the step defaults to 1 (or -1 when start > end); with
	// 3 args the explicit step is used. Step zero is a runtime
	// error.
	OpRange
	// OpAddString is the type-specialised string-concat opcode the
	// compiler emits when it can prove both operands of `+` are
	// statically typed `string`. The handler does a direct
	// `runtime.String.Value` concat with no method-dispatch detour
	// and no type switch. Mirrors the `OpAddInt` family for ints.
	OpAddString
	// OpCallResolvedMethod skips classInfo / lookupMethodLower /
	// selectRuntimeFunction when the compiler proved the receiver's
	// class statically. Operands [functionIndex, argc]; stack
	// [receiver, arg0, ..., argN-1].
	OpCallResolvedMethod
	// OpAddStringConst {literalConstantIndex}: pops the dynamic
	// left operand, concatenates it with chunk.Constants[idx] (the
	// baked-in literal), pushes the result. Emitted for the
	// `acc + "x"` pattern.
	OpAddStringConst
	// OpAppendStringConst {localSlot, literalConstantIndex}: reads
	// the local at slot, concatenates with chunk.Constants[idx],
	// writes back to the same slot. Fused form of
	// `local = local + "literal"`.
	OpAppendStringConst
	// OpAppendGlobalStringConst {globalSlot, literalConstantIndex}:
	// global-slot variant of OpAppendStringConst.
	OpAppendGlobalStringConst
	// OpAppendStringConstStmt: builder-backed, no stack push.
	// Emitted for `local = local + "literal"` as a standalone stmt.
	OpAppendStringConstStmt
	OpAppendGlobalStringConstStmt
	// OpTailCall {functionIndex, argc}: emitted for `return f(args)` in
	// tail position. Reuses the current frame instead of pushing a
	// new one; the eventual OpReturn pops the original caller's frame.
	// Skipped by the compiler if defers / exception handlers / iterator
	// finalizers are in scope or the call cannot be statically resolved.
	OpTailCall
	// OpDeferNativeCallNamed {nameIndex, argc, argName0Index, ...}:
	// like OpDeferNativeCall but carries per-arg name indices so
	// `defer module.fn(b=2, a=1)` captures the named-arg shape and
	// the deferred dispatch can reorder against the native registry's
	// signature when the queue runs. argNameIndex < 0 marks a
	// positional arg.
	OpDeferNativeCallNamed
	// OpDeferMethodCallNamed {nameIndex, argc, argName0Index, ...}:
	// named-arg counterpart to OpDeferMethodCall.
	OpDeferMethodCallNamed
	// OpDeferCallableCallNamed {argc, argName0Index, ...}: named-arg
	// counterpart to OpDeferCallableCall. The closure's signature is
	// resolved at deferred-dispatch time so the names can be reordered
	// against the underlying function's ParamNames.
	OpDeferCallableCallNamed
	OpImportFrom
	// OpFormatSpec applies a Python-style format spec to the value at
	// the top of the stack. Pops (value, spec-string), pushes the
	// formatted string. Emitted for `${expr:spec}` interpolations.
	OpFormatSpec
	// OpDir pops a value and pushes the sorted list of method names
	// callable on it (the dir(value) builtin). The no-argument and
	// module-introspection forms of dir are evaluator/REPL-only.
	OpDir
	// OpDump pops a value and pushes its type-annotated debug string
	// (the dump(value) builtin).
	OpDump
	// OpSelect runs a select-statement dispatch over the cases pushed
	// on the stack. Operands: numCases, hasDefault (0/1), one
	// per-case triple (kindCode 0=recv 1=send, bodyOffset,
	// bindingSlot or -1), optional defaultOffset. The VM pops
	// (handle, value) for each case and runs reflect.Select, then
	// jumps to the chosen body. For a recv case with a binding the
	// received value is stored in bindingSlot before the jump.
	OpSelect
	// OpNativeValue pushes a pure native builtin (e.g. math.abs) as a
	// first-class callable value. Operands: canonical-name const index,
	// function-name const index.
	OpNativeValue
	// OpFreezeLocal freezes a local slot's value (const-parameter prologue).
	OpFreezeLocal
	// OpCheckUnpackLen errors when a list is too short to destructure.
	// Operands: [tempSlot, expectedCount].
	OpCheckUnpackLen
	// OpContains implements the `in` operator: pops container then needle,
	// pushes a Bool. Dispatches to __contains for user objects.
	OpContains
	// Spread-aware OpNativeCall. Operands [nameIndex, staticArgCount];
	// stack [static_args..., spread_list].
	OpNativeCallSpread
	// Spread-aware OpConstructClass. Operands [classIndex, staticArgCount];
	// stack [static_args..., spread_list_or_dict].
	OpConstructClassSpread
	// Spread-aware OpCallStaticMethod. Operands [classIndex, nameIndex,
	// staticArgCount]; stack [static_args..., spread_list_or_dict].
	OpCallStaticMethodSpread
	// OpZRange is OpRange with an exclusive end and a 1-arg form
	// (zrange(n) ranges 0..n). Argument count operand is 1, 2, or 3.
	OpZRange
	// OpMakeOverloaded builds a runtime.OverloadedFunction value from its operand function indices, so an overloaded function used as a value keeps every overload for call-time selection.
	OpMakeOverloaded
	// OpDeferFuncCallSpread {funcIdx, argc, meta...}: spread-aware OpDeferFuncCall; stack [args...], spread lists read at fire time.
	OpDeferFuncCallSpread
	// OpDeferMethodCallSpread {nameIndex, argc, meta...}: spread-aware OpDeferMethodCall; stack [receiver, args...].
	OpDeferMethodCallSpread
	// OpDeferNativeCallSpread {nameIndex, argc, meta...}: spread-aware OpDeferNativeCall; list spread only, matching OpNativeCallSpread.
	OpDeferNativeCallSpread
	// OpDeferCallableCallSpread {argc, meta...}: spread-aware OpDeferCallableCall; stack [callable, args...].
	OpDeferCallableCallSpread
)

type Instruction struct {
	Operands []int64
	Op       Op
	// Line/Column are sized so the struct stays 32 bytes; positions
	// only feed error rendering, where clamping is harmless.
	Column uint16
	Line   int32
}

// clampColumn saturates parser columns into the packed uint16 field.
func clampColumn(column int) uint16 {
	if column < 0 {
		return 0
	}
	if column > 65535 {
		return 65535
	}
	return uint16(column)
}

type Chunk struct {
	SourceHash   [32]byte
	Compiler     string
	Constants    []runtime.Value
	Instructions []Instruction
	Functions    []FunctionInfo
	// sharedMeta carries the once-prepared runtime metadata (memoised
	// function specs, class index, wrapper templates) shared by every
	// VM bound to a copy of this chunk. Set at Compile/Decode; nil for
	// hand-built chunks, which fall back to per-VM preparation.
	sharedMeta *chunkSharedMeta
	Classes    []ClassInfo
	Interfaces []InterfaceInfo
	Exports    []ExportInfo
	// TopLevelLocalCount is the number of local slots reachable from the
	// chunk's top-level execution (outside any function body). The VM
	// pre-sizes vm.locals to this value at Run() entry so the hot
	// OpGetLocal / OpSetLocal handlers can skip bounds checks.
	TopLevelLocalCount int64
	// GlobalCount is the total number of global slots used by the
	// compiled chunk. The VM pre-sizes vm.globals accordingly so the hot
	// OpGetGlobal / OpSetGlobal handlers can skip bounds checks.
	GlobalCount int64
	operandPool []int64 // contiguous backing store for all Instruction.Operands slices
}

// consolidateOperands packs all instruction operand data into a single contiguous
// slab (operandPool) and updates each Instruction.Operands to point into it.
// This improves cache locality during VM dispatch at no change to the public API.
func (chunk *Chunk) consolidateOperands() {
	total := 0
	for i := range chunk.Instructions {
		total += len(chunk.Instructions[i].Operands)
	}
	if total == 0 {
		return
	}
	pool := make([]int64, 0, total)
	for i := range chunk.Instructions {
		ins := &chunk.Instructions[i]
		if len(ins.Operands) == 0 {
			continue
		}
		start := len(pool)
		pool = append(pool, ins.Operands...)
		ins.Operands = pool[start : start+len(ins.Operands) : start+len(ins.Operands)]
	}
	chunk.operandPool = pool
}

type FunctionInfo struct {
	Name                     string
	Doc                      string
	TypeParameters           []string
	TypeParamConstraintExprs []string // parallel to TypeParameters; "" if no constraint
	Entry                    int64
	ParamNames               []string
	ParamSlots               []int64
	ParamTypes               []string
	ParamDecorators          [][]runtime.DecoratorMetadata
	ReturnType               string
	DefaultConstants         []int64
	UpvalueCount             int64
	LocalCount               int64 // total local slots needed; pre-allocated at call entry
	Variadic                 bool
	Async                    bool
	IsGenerator              bool
	// SharesParentFrame is set for nested function statements declared
	// inside another function's body. Their bodies may reference outer
	// locals directly via the outer's absolute slot numbers, so the VM
	// runs them on the caller's locals buffer instead of allocating a
	// fresh frame slice.
	SharesParentFrame bool
	// DefLine / DefColumn capture the source position of the `func`
	// keyword for this function, exposed by reflect.location.
	DefLine        int64
	DefColumn      int64
	Decorators     []runtime.DecoratorMetadata
	paramTypeSpecs []vmTypeSpec
	typeParamSet   map[string]bool
	// False when every ParamTypes entry is "" or "any"; lets call
	// entry skip the validation walk for dynamically-typed funcs.
	requiresParamValidation bool
}

type ClassInfo struct {
	Name        string
	Doc         string
	ParentIndex int64
	ParentName  string
	// ParentArguments captures the type arguments supplied to the
	// parent in the extends clause (e.g. for `extends Base<string, int>`
	// this is ["string", "int"]). Empty when the parent is non-generic
	// or no explicit type arguments are given.
	ParentArguments          []string
	TypeParameters           []string
	TypeParamConstraintExprs []string // parallel to TypeParameters; "" if no constraint
	FieldNames               []string
	// FieldTypes parallels FieldNames - the declared type string of
	// each field, or "" when untyped. Populated at compile time so
	// reflect.fields can report types without re-reading the AST.
	FieldTypes []string
	// FieldDocs parallels FieldNames - each field's docblock (or "") for reflect.fields.
	FieldDocs []string
	// FieldDecorators parallels FieldNames - the metadata for any
	// @-prefixed annotations on the field declaration (e.g.
	// `@Assert.email`). Empty per field by default. Populated at
	// compile time; consumed by `reflect.fields` so frameworks can
	// drive validation / serialization off the field annotations.
	FieldDecorators    [][]runtime.DecoratorMetadata
	FieldDefaults      []int64
	ConstructorIndices []int64
	// DestructorIndex is the function index of `func ~ClassName()`,
	// or -1 when the class has no destructor.
	DestructorIndex  int64
	Methods          map[string][]int64
	StaticValues     map[string]int64
	StaticMethods    map[string][]int64
	Implements       []string
	Decorators       []runtime.DecoratorMetadata
	MethodDecorators map[string][]runtime.DecoratorMetadata
	StaticDecorators map[string][]runtime.DecoratorMetadata
	Immutable        bool
	// ImmutableFields names the fields declared `@immutable` on this class:
	// set-once, writable during construction and locked afterwards.
	ImmutableFields []string
	// DefLine / DefColumn capture the source position of the `class`
	// keyword, exposed by reflect.location.
	DefLine   int64
	DefColumn int64
}

type InterfaceInfo struct {
	Name           string
	Doc            string
	TypeParameters []string
	Parents        []string
	Methods        []runtime.FunctionMetadata
	Defaults       map[string]int64
	Fields         []string
	FieldTypes     []string
}

type ExportInfo struct {
	Name           string
	Slot           int64
	FunctionIndex  int64
	ClassIndex     int64
	InterfaceIndex int64
	// Set (with FunctionIndex -1) when the export names an overloaded free function; carries every overload's chunk index.
	OverloadIndices []int64
}

func SourceHash(source []byte) [32]byte {
	return sha256.Sum256(source)
}

func Encode(chunk Chunk) ([]byte, error) {
	out := make([]byte, 0, 64+len(chunk.Instructions)*8)
	out = append(out, []byte(Magic)...)
	out = binary.BigEndian.AppendUint16(out, Version)
	out = append(out, chunk.SourceHash[:]...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(chunk.Compiler)))
	out = append(out, []byte(chunk.Compiler)...)
	out = binary.BigEndian.AppendUint64(out, uint64(chunk.TopLevelLocalCount))
	out = binary.BigEndian.AppendUint64(out, uint64(chunk.GlobalCount))
	out = binary.BigEndian.AppendUint32(out, uint32(len(chunk.Constants)))
	for _, constant := range chunk.Constants {
		var err error
		out, err = appendConstant(out, constant)
		if err != nil {
			return nil, err
		}
	}
	out = binary.BigEndian.AppendUint32(out, uint32(len(chunk.Instructions)))
	for _, instruction := range chunk.Instructions {
		out = append(out, byte(instruction.Op))
		out = binary.BigEndian.AppendUint16(out, uint16(instruction.Line))
		out = binary.BigEndian.AppendUint16(out, uint16(instruction.Column))
		out = binary.BigEndian.AppendUint16(out, uint16(len(instruction.Operands)))
		for _, operand := range instruction.Operands {
			out = binary.BigEndian.AppendUint64(out, uint64(operand))
		}
	}
	out = binary.BigEndian.AppendUint32(out, uint32(len(chunk.Functions)))
	for _, function := range chunk.Functions {
		out = binary.BigEndian.AppendUint16(out, uint16(len(function.Name)))
		out = append(out, []byte(function.Name)...)
		out = appendString(out, function.Doc)
		out = binary.BigEndian.AppendUint16(out, uint16(len(function.TypeParameters)))
		for _, name := range function.TypeParameters {
			out = binary.BigEndian.AppendUint16(out, uint16(len(name)))
			out = append(out, []byte(name)...)
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(function.TypeParamConstraintExprs)))
		for _, expr := range function.TypeParamConstraintExprs {
			out = binary.BigEndian.AppendUint16(out, uint16(len(expr)))
			out = append(out, []byte(expr)...)
		}
		out = binary.BigEndian.AppendUint64(out, uint64(function.Entry))
		out = binary.BigEndian.AppendUint16(out, uint16(len(function.ParamNames)))
		for _, name := range function.ParamNames {
			out = binary.BigEndian.AppendUint16(out, uint16(len(name)))
			out = append(out, []byte(name)...)
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(function.ParamSlots)))
		for _, slot := range function.ParamSlots {
			out = binary.BigEndian.AppendUint64(out, uint64(slot))
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(function.ParamTypes)))
		for _, typ := range function.ParamTypes {
			out = binary.BigEndian.AppendUint16(out, uint16(len(typ)))
			out = append(out, []byte(typ)...)
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(function.ParamDecorators)))
		for _, decs := range function.ParamDecorators {
			var err error
			out, err = appendDecoratorMetadata(out, decs)
			if err != nil {
				return nil, err
			}
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(function.ReturnType)))
		out = append(out, []byte(function.ReturnType)...)
		out = binary.BigEndian.AppendUint16(out, uint16(len(function.DefaultConstants)))
		for _, index := range function.DefaultConstants {
			out = binary.BigEndian.AppendUint64(out, uint64(index))
		}
		out = binary.BigEndian.AppendUint64(out, uint64(function.UpvalueCount))
		out = binary.BigEndian.AppendUint64(out, uint64(function.LocalCount))
		variadicByte := byte(0)
		if function.Variadic {
			variadicByte = 1
		}
		out = append(out, variadicByte)
		asyncByte := byte(0)
		if function.Async {
			asyncByte = 1
		}
		out = append(out, asyncByte)
		generatorByte := byte(0)
		if function.IsGenerator {
			generatorByte = 1
		}
		out = append(out, generatorByte)
		sharesByte := byte(0)
		if function.SharesParentFrame {
			sharesByte = 1
		}
		out = append(out, sharesByte)
		out = binary.BigEndian.AppendUint64(out, uint64(function.DefLine))
		out = binary.BigEndian.AppendUint64(out, uint64(function.DefColumn))
		var err error
		out, err = appendDecoratorMetadata(out, function.Decorators)
		if err != nil {
			return nil, err
		}
	}
	out = binary.BigEndian.AppendUint32(out, uint32(len(chunk.Classes)))
	for _, class := range chunk.Classes {
		out = binary.BigEndian.AppendUint16(out, uint16(len(class.Name)))
		out = append(out, []byte(class.Name)...)
		out = appendString(out, class.Doc)
		out = binary.BigEndian.AppendUint16(out, uint16(len(class.ParentName)))
		out = append(out, []byte(class.ParentName)...)
		out = binary.BigEndian.AppendUint64(out, uint64(class.ParentIndex))
		out = binary.BigEndian.AppendUint16(out, uint16(len(class.ParentArguments)))
		for _, name := range class.ParentArguments {
			out = binary.BigEndian.AppendUint16(out, uint16(len(name)))
			out = append(out, []byte(name)...)
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(class.TypeParameters)))
		for _, name := range class.TypeParameters {
			out = binary.BigEndian.AppendUint16(out, uint16(len(name)))
			out = append(out, []byte(name)...)
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(class.TypeParamConstraintExprs)))
		for _, expr := range class.TypeParamConstraintExprs {
			out = binary.BigEndian.AppendUint16(out, uint16(len(expr)))
			out = append(out, []byte(expr)...)
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(class.FieldNames)))
		for i, name := range class.FieldNames {
			out = binary.BigEndian.AppendUint16(out, uint16(len(name)))
			out = append(out, []byte(name)...)
			out = binary.BigEndian.AppendUint64(out, uint64(class.FieldDefaults[i]))
			fieldType := ""
			if i < len(class.FieldTypes) {
				fieldType = class.FieldTypes[i]
			}
			out = binary.BigEndian.AppendUint16(out, uint16(len(fieldType)))
			out = append(out, []byte(fieldType)...)
			fieldDoc := ""
			if i < len(class.FieldDocs) {
				fieldDoc = class.FieldDocs[i]
			}
			out = binary.BigEndian.AppendUint16(out, uint16(len(fieldDoc)))
			out = append(out, []byte(fieldDoc)...)
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(class.ConstructorIndices)))
		for _, index := range class.ConstructorIndices {
			out = binary.BigEndian.AppendUint64(out, uint64(index))
		}
		out = binary.BigEndian.AppendUint64(out, uint64(class.DestructorIndex))
		out = binary.BigEndian.AppendUint16(out, uint16(len(class.Methods)))
		for name, indices := range class.Methods {
			out = binary.BigEndian.AppendUint16(out, uint16(len(name)))
			out = append(out, []byte(name)...)
			out = binary.BigEndian.AppendUint16(out, uint16(len(indices)))
			for _, index := range indices {
				out = binary.BigEndian.AppendUint64(out, uint64(index))
			}
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(class.StaticValues)))
		for name, index := range class.StaticValues {
			out = binary.BigEndian.AppendUint16(out, uint16(len(name)))
			out = append(out, []byte(name)...)
			out = binary.BigEndian.AppendUint64(out, uint64(index))
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(class.StaticMethods)))
		for name, indices := range class.StaticMethods {
			out = binary.BigEndian.AppendUint16(out, uint16(len(name)))
			out = append(out, []byte(name)...)
			out = binary.BigEndian.AppendUint16(out, uint16(len(indices)))
			for _, index := range indices {
				out = binary.BigEndian.AppendUint64(out, uint64(index))
			}
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(class.Implements)))
		for _, name := range class.Implements {
			out = binary.BigEndian.AppendUint16(out, uint16(len(name)))
			out = append(out, []byte(name)...)
		}
		var err error
		out, err = appendDecoratorMetadata(out, class.Decorators)
		if err != nil {
			return nil, err
		}
		out, err = appendDecoratorMetadataMap(out, class.MethodDecorators)
		if err != nil {
			return nil, err
		}
		out, err = appendDecoratorMetadataMap(out, class.StaticDecorators)
		if err != nil {
			return nil, err
		}
		/* Per-field decorators, parallel to FieldNames. Length is
		 * either 0 (no field has decorators) or len(FieldNames). */
		out = binary.BigEndian.AppendUint16(out, uint16(len(class.FieldDecorators)))
		for _, decs := range class.FieldDecorators {
			out, err = appendDecoratorMetadata(out, decs)
			if err != nil {
				return nil, err
			}
		}
		immByte := byte(0)
		if class.Immutable {
			immByte = 1
		}
		out = append(out, immByte)
		out = binary.BigEndian.AppendUint16(out, uint16(len(class.ImmutableFields)))
		for _, name := range class.ImmutableFields {
			out = binary.BigEndian.AppendUint16(out, uint16(len(name)))
			out = append(out, []byte(name)...)
		}
		out = binary.BigEndian.AppendUint64(out, uint64(class.DefLine))
		out = binary.BigEndian.AppendUint64(out, uint64(class.DefColumn))
	}
	out = binary.BigEndian.AppendUint32(out, uint32(len(chunk.Interfaces)))
	for _, iface := range chunk.Interfaces {
		out = binary.BigEndian.AppendUint16(out, uint16(len(iface.Name)))
		out = append(out, []byte(iface.Name)...)
		out = appendString(out, iface.Doc)
		out = binary.BigEndian.AppendUint16(out, uint16(len(iface.TypeParameters)))
		for _, name := range iface.TypeParameters {
			out = binary.BigEndian.AppendUint16(out, uint16(len(name)))
			out = append(out, []byte(name)...)
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(iface.Parents)))
		for _, parent := range iface.Parents {
			out = binary.BigEndian.AppendUint16(out, uint16(len(parent)))
			out = append(out, []byte(parent)...)
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(iface.Methods)))
		for _, method := range iface.Methods {
			var err error
			out, err = appendFunctionMetadata(out, &method)
			if err != nil {
				return nil, err
			}
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(iface.Defaults)))
		defaultNames := make([]string, 0, len(iface.Defaults))
		for name := range iface.Defaults {
			defaultNames = append(defaultNames, name)
		}
		sort.Strings(defaultNames)
		for _, name := range defaultNames {
			out = binary.BigEndian.AppendUint16(out, uint16(len(name)))
			out = append(out, []byte(name)...)
			out = binary.BigEndian.AppendUint64(out, uint64(iface.Defaults[name]))
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(iface.Fields)))
		for i, name := range iface.Fields {
			out = binary.BigEndian.AppendUint16(out, uint16(len(name)))
			out = append(out, []byte(name)...)
			typeStr := ""
			if i < len(iface.FieldTypes) {
				typeStr = iface.FieldTypes[i]
			}
			out = binary.BigEndian.AppendUint16(out, uint16(len(typeStr)))
			out = append(out, []byte(typeStr)...)
		}
	}
	out = binary.BigEndian.AppendUint16(out, uint16(len(chunk.Exports)))
	for _, export := range chunk.Exports {
		out = binary.BigEndian.AppendUint16(out, uint16(len(export.Name)))
		out = append(out, []byte(export.Name)...)
		out = binary.BigEndian.AppendUint64(out, uint64(export.Slot))
		out = binary.BigEndian.AppendUint64(out, uint64(export.FunctionIndex))
		out = binary.BigEndian.AppendUint64(out, uint64(export.ClassIndex))
		out = binary.BigEndian.AppendUint64(out, uint64(export.InterfaceIndex))
		out = binary.BigEndian.AppendUint16(out, uint16(len(export.OverloadIndices)))
		for _, idx := range export.OverloadIndices {
			out = binary.BigEndian.AppendUint64(out, uint64(idx))
		}
	}
	return out, nil
}

func Decode(data []byte) (Chunk, error) {
	reader := byteReader{data: data}
	if string(reader.read(len(Magic))) != Magic {
		return Chunk{}, errors.New("invalid bytecode magic")
	}
	version := reader.u16()
	if version != Version {
		return Chunk{}, fmt.Errorf("unsupported bytecode version %d", version)
	}
	var chunk Chunk
	copy(chunk.SourceHash[:], reader.read(32))
	compilerLen := int(reader.u16())
	chunk.Compiler = string(reader.read(compilerLen))
	chunk.TopLevelLocalCount = int64(reader.u64())
	chunk.GlobalCount = int64(reader.u64())
	constantCount := int(reader.u32())
	chunk.Constants = make([]runtime.Value, 0, constantCount)
	for i := 0; i < constantCount; i++ {
		value := reader.constant()
		chunk.Constants = append(chunk.Constants, value)
	}
	instructionCount := int(reader.u32())
	chunk.Instructions = make([]Instruction, 0, instructionCount)
	for i := 0; i < instructionCount; i++ {
		instruction := Instruction{
			Op:     Op(reader.u8()),
			Line:   int32(reader.u16()),
			Column: reader.u16(),
		}
		operandCount := int(reader.u16())
		instruction.Operands = make([]int64, 0, operandCount)
		for j := 0; j < operandCount; j++ {
			instruction.Operands = append(instruction.Operands, int64(reader.u64()))
		}
		chunk.Instructions = append(chunk.Instructions, instruction)
	}
	functionCount := int(reader.u32())
	chunk.Functions = make([]FunctionInfo, 0, functionCount)
	for i := 0; i < functionCount; i++ {
		function := FunctionInfo{
			Name: string(reader.read(int(reader.u16()))),
		}
		function.Doc = reader.string()
		typeParamCount := int(reader.u16())
		function.TypeParameters = make([]string, 0, typeParamCount)
		for j := 0; j < typeParamCount; j++ {
			function.TypeParameters = append(function.TypeParameters, string(reader.read(int(reader.u16()))))
		}
		constraintExprCount := int(reader.u16())
		function.TypeParamConstraintExprs = make([]string, 0, constraintExprCount)
		for j := 0; j < constraintExprCount; j++ {
			function.TypeParamConstraintExprs = append(function.TypeParamConstraintExprs, string(reader.read(int(reader.u16()))))
		}
		function.Entry = int64(reader.u64())
		nameCount := int(reader.u16())
		function.ParamNames = make([]string, 0, nameCount)
		for j := 0; j < nameCount; j++ {
			function.ParamNames = append(function.ParamNames, strings.ToLower(string(reader.read(int(reader.u16())))))
		}
		paramCount := int(reader.u16())
		function.ParamSlots = make([]int64, 0, paramCount)
		for j := 0; j < paramCount; j++ {
			function.ParamSlots = append(function.ParamSlots, int64(reader.u64()))
		}
		typeCount := int(reader.u16())
		function.ParamTypes = make([]string, 0, typeCount)
		for j := 0; j < typeCount; j++ {
			function.ParamTypes = append(function.ParamTypes, string(reader.read(int(reader.u16()))))
		}
		paramDecCount := int(reader.u16())
		if paramDecCount > 0 {
			function.ParamDecorators = make([][]runtime.DecoratorMetadata, 0, paramDecCount)
			for j := 0; j < paramDecCount; j++ {
				function.ParamDecorators = append(function.ParamDecorators, reader.decoratorMetadata())
			}
		}
		function.ReturnType = string(reader.read(int(reader.u16())))
		defaultCount := int(reader.u16())
		function.DefaultConstants = make([]int64, 0, defaultCount)
		for j := 0; j < defaultCount; j++ {
			function.DefaultConstants = append(function.DefaultConstants, int64(reader.u64()))
		}
		function.UpvalueCount = int64(reader.u64())
		function.LocalCount = int64(reader.u64())
		function.Variadic = reader.u8() != 0
		function.Async = reader.u8() != 0
		function.IsGenerator = reader.u8() != 0
		function.SharesParentFrame = reader.u8() != 0
		function.DefLine = int64(reader.u64())
		function.DefColumn = int64(reader.u64())
		function.Decorators = reader.decoratorMetadata()
		chunk.Functions = append(chunk.Functions, function)
	}
	classCount := int(reader.u32())
	chunk.Classes = make([]ClassInfo, 0, classCount)
	for i := 0; i < classCount; i++ {
		class := ClassInfo{
			Name:            string(reader.read(int(reader.u16()))),
			Doc:             reader.string(),
			ParentName:      string(reader.read(int(reader.u16()))),
			ParentIndex:     int64(reader.u64()),
			DestructorIndex: -1,
			Methods:         map[string][]int64{},
			StaticValues:    map[string]int64{},
			StaticMethods:   map[string][]int64{},
		}
		parentArgCount := int(reader.u16())
		class.ParentArguments = make([]string, 0, parentArgCount)
		for j := 0; j < parentArgCount; j++ {
			class.ParentArguments = append(class.ParentArguments, string(reader.read(int(reader.u16()))))
		}
		typeParamCount := int(reader.u16())
		class.TypeParameters = make([]string, 0, typeParamCount)
		for j := 0; j < typeParamCount; j++ {
			class.TypeParameters = append(class.TypeParameters, string(reader.read(int(reader.u16()))))
		}
		constraintExprCount := int(reader.u16())
		class.TypeParamConstraintExprs = make([]string, 0, constraintExprCount)
		for j := 0; j < constraintExprCount; j++ {
			class.TypeParamConstraintExprs = append(class.TypeParamConstraintExprs, string(reader.read(int(reader.u16()))))
		}
		fieldCount := int(reader.u16())
		class.FieldNames = make([]string, 0, fieldCount)
		class.FieldDefaults = make([]int64, 0, fieldCount)
		class.FieldTypes = make([]string, 0, fieldCount)
		class.FieldDocs = make([]string, 0, fieldCount)
		for j := 0; j < fieldCount; j++ {
			class.FieldNames = append(class.FieldNames, string(reader.read(int(reader.u16()))))
			class.FieldDefaults = append(class.FieldDefaults, int64(reader.u64()))
			class.FieldTypes = append(class.FieldTypes, string(reader.read(int(reader.u16()))))
			class.FieldDocs = append(class.FieldDocs, string(reader.read(int(reader.u16()))))
		}
		constructorCount := int(reader.u16())
		class.ConstructorIndices = make([]int64, 0, constructorCount)
		for j := 0; j < constructorCount; j++ {
			class.ConstructorIndices = append(class.ConstructorIndices, int64(reader.u64()))
		}
		class.DestructorIndex = int64(reader.u64())
		methodCount := int(reader.u16())
		for j := 0; j < methodCount; j++ {
			name := strings.ToLower(string(reader.read(int(reader.u16()))))
			indexCount := int(reader.u16())
			indices := make([]int64, 0, indexCount)
			for k := 0; k < indexCount; k++ {
				indices = append(indices, int64(reader.u64()))
			}
			class.Methods[name] = indices
		}
		staticValueCount := int(reader.u16())
		for j := 0; j < staticValueCount; j++ {
			class.StaticValues[string(reader.read(int(reader.u16())))] = int64(reader.u64())
		}
		staticMethodCount := int(reader.u16())
		for j := 0; j < staticMethodCount; j++ {
			name := strings.ToLower(string(reader.read(int(reader.u16()))))
			indexCount := int(reader.u16())
			indices := make([]int64, 0, indexCount)
			for k := 0; k < indexCount; k++ {
				indices = append(indices, int64(reader.u64()))
			}
			class.StaticMethods[name] = indices
		}
		implementsCount := int(reader.u16())
		class.Implements = make([]string, 0, implementsCount)
		for j := 0; j < implementsCount; j++ {
			class.Implements = append(class.Implements, string(reader.read(int(reader.u16()))))
		}
		class.Decorators = reader.decoratorMetadata()
		class.MethodDecorators = reader.decoratorMetadataMap()
		class.StaticDecorators = reader.decoratorMetadataMap()
		fieldDecCount := int(reader.u16())
		if fieldDecCount > 0 {
			class.FieldDecorators = make([][]runtime.DecoratorMetadata, 0, fieldDecCount)
			for j := 0; j < fieldDecCount; j++ {
				class.FieldDecorators = append(class.FieldDecorators, reader.decoratorMetadata())
			}
		}
		class.Immutable = reader.u8() != 0
		immCount := int(reader.u16())
		if immCount > 0 {
			class.ImmutableFields = make([]string, 0, immCount)
			for j := 0; j < immCount; j++ {
				class.ImmutableFields = append(class.ImmutableFields, string(reader.read(int(reader.u16()))))
			}
		}
		class.DefLine = int64(reader.u64())
		class.DefColumn = int64(reader.u64())
		chunk.Classes = append(chunk.Classes, class)
	}
	interfaceCount := int(reader.u32())
	chunk.Interfaces = make([]InterfaceInfo, 0, interfaceCount)
	for i := 0; i < interfaceCount; i++ {
		iface := InterfaceInfo{Name: string(reader.read(int(reader.u16())))}
		iface.Doc = reader.string()
		typeParamCount := int(reader.u16())
		iface.TypeParameters = make([]string, 0, typeParamCount)
		for j := 0; j < typeParamCount; j++ {
			iface.TypeParameters = append(iface.TypeParameters, string(reader.read(int(reader.u16()))))
		}
		parentCount := int(reader.u16())
		iface.Parents = make([]string, 0, parentCount)
		for j := 0; j < parentCount; j++ {
			iface.Parents = append(iface.Parents, string(reader.read(int(reader.u16()))))
		}
		methodCount := int(reader.u16())
		iface.Methods = make([]runtime.FunctionMetadata, 0, methodCount)
		for j := 0; j < methodCount; j++ {
			metadata := reader.functionMetadata()
			if metadata != nil {
				iface.Methods = append(iface.Methods, *metadata)
			}
		}
		defaultCount := int(reader.u16())
		if defaultCount > 0 {
			iface.Defaults = make(map[string]int64, defaultCount)
			for j := 0; j < defaultCount; j++ {
				name := string(reader.read(int(reader.u16())))
				iface.Defaults[name] = int64(reader.u64())
			}
		}
		fieldCount := int(reader.u16())
		iface.Fields = make([]string, 0, fieldCount)
		iface.FieldTypes = make([]string, 0, fieldCount)
		for j := 0; j < fieldCount; j++ {
			iface.Fields = append(iface.Fields, string(reader.read(int(reader.u16()))))
			iface.FieldTypes = append(iface.FieldTypes, string(reader.read(int(reader.u16()))))
		}
		chunk.Interfaces = append(chunk.Interfaces, iface)
	}
	exportCount := int(reader.u16())
	chunk.Exports = make([]ExportInfo, 0, exportCount)
	for i := 0; i < exportCount; i++ {
		export := ExportInfo{
			Name:           string(reader.read(int(reader.u16()))),
			Slot:           int64(reader.u64()),
			FunctionIndex:  int64(reader.u64()),
			ClassIndex:     int64(reader.u64()),
			InterfaceIndex: int64(reader.u64()),
		}
		if overloadCount := int(reader.u16()); overloadCount > 0 {
			export.OverloadIndices = make([]int64, overloadCount)
			for j := range export.OverloadIndices {
				export.OverloadIndices[j] = int64(reader.u64())
			}
		}
		chunk.Exports = append(chunk.Exports, export)
	}
	if reader.err != nil {
		return Chunk{}, reader.err
	}
	if reader.pos != len(data) {
		return Chunk{}, errors.New("trailing bytecode data")
	}
	chunk.consolidateOperands()
	chunk.sharedMeta = newChunkSharedMeta()
	return chunk, nil
}

func appendConstant(out []byte, value runtime.Value) ([]byte, error) {
	switch value := value.(type) {
	case runtime.Null:
		return append(out, 0), nil
	case runtime.Bool:
		out = append(out, 1)
		if value.Value {
			return append(out, 1), nil
		}
		return append(out, 0), nil
	case runtime.SmallInt:
		out = append(out, 2)
		text := strconv.FormatInt(value.Value, 10)
		out = binary.BigEndian.AppendUint32(out, uint32(len(text)))
		return append(out, []byte(text)...), nil
	case runtime.Int:
		out = append(out, 2)
		text := value.Value.String()
		out = binary.BigEndian.AppendUint32(out, uint32(len(text)))
		return append(out, []byte(text)...), nil
	case runtime.String:
		out = append(out, 3)
		out = binary.BigEndian.AppendUint32(out, uint32(len(value.Value)))
		return append(out, []byte(value.Value)...), nil
	case runtime.Decimal:
		out = append(out, 4)
		text := value.Value.RatString()
		out = binary.BigEndian.AppendUint32(out, uint32(len(text)))
		return append(out, []byte(text)...), nil
	case runtime.Float:
		out = append(out, 5)
		return binary.BigEndian.AppendUint64(out, math.Float64bits(value.Value)), nil
	case runtime.DecoratorTarget:
		out = append(out, 6)
		out = appendString(out, value.Target)
		out, err := appendFunctionMetadata(out, value.Function)
		if err != nil {
			return nil, err
		}
		out = appendClassMetadata(out, value.Class)
		out = binary.BigEndian.AppendUint16(out, uint16(len(value.Decorators)))
		for _, decorator := range value.Decorators {
			out = appendString(out, decorator.Name)
			out = appendString(out, decorator.Target)
			out = binary.BigEndian.AppendUint64(out, uint64(decorator.Position))
			out = binary.BigEndian.AppendUint64(out, uint64(decorator.Overload))
			out = binary.BigEndian.AppendUint64(out, uint64(decorator.Line))
			out = binary.BigEndian.AppendUint64(out, uint64(decorator.Column))
			out = binary.BigEndian.AppendUint16(out, uint16(len(decorator.Args)))
			for _, arg := range decorator.Args {
				var err error
				out, err = appendMetadataValue(out, arg)
				if err != nil {
					return nil, err
				}
			}
			out = binary.BigEndian.AppendUint16(out, uint16(len(decorator.NamedArgs)))
			for name, arg := range decorator.NamedArgs {
				out = appendString(out, name)
				var err error
				out, err = appendMetadataValue(out, arg)
				if err != nil {
					return nil, err
				}
			}
		}
		return out, nil
	case *runtime.EnumDef:
		out = append(out, 7)
		out = appendString(out, value.Name)
		out = appendString(out, value.Module)
		out = appendString(out, value.BackingType)
		out = binary.BigEndian.AppendUint16(out, uint16(len(value.Variants)))
		for _, v := range value.Variants {
			out = appendString(out, v.Name)
			out = binary.BigEndian.AppendUint16(out, uint16(v.FieldCount))
			if v.BackingValue == nil {
				out = append(out, 0)
			} else {
				out = append(out, 1)
				var err error
				out, err = appendMetadataValue(out, v.BackingValue)
				if err != nil {
					return nil, err
				}
			}
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(value.Implements)))
		for _, name := range value.Implements {
			out = appendString(out, name)
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(value.MethodIndices)))
		for name, indices := range value.MethodIndices {
			out = appendString(out, name)
			out = binary.BigEndian.AppendUint16(out, uint16(len(indices)))
			for _, index := range indices {
				out = binary.BigEndian.AppendUint64(out, uint64(index))
			}
		}
		return out, nil
	case runtime.Type:
		out = append(out, 8)
		return appendString(out, value.Name), nil
	case runtime.BytecodeClass:
		out = append(out, 9)
		out = appendString(out, value.Name)
		out = appendString(out, value.Doc)
		out = binary.BigEndian.AppendUint64(out, uint64(value.Index))
		out, err := appendDecoratorMetadata(out, value.Decorators)
		if err != nil {
			return nil, err
		}
		out, err = appendDecoratorMetadataMap(out, value.MethodDecorators)
		if err != nil {
			return nil, err
		}
		out, err = appendDecoratorMetadataMap(out, value.StaticDecorators)
		if err != nil {
			return nil, err
		}
		return out, nil
	case runtime.Dict:
		/* Only empty dicts reach the constant pool today, via
		 * `dict opts = {}` defaults. Encode just the empty marker;
		 * non-empty constant dicts would need element serialisation
		 * that the compiler doesn't emit. */
		if value.Len() != 0 {
			return nil, fmt.Errorf("unsupported bytecode constant: non-empty dict literal")
		}
		return append(out, 10), nil
	case *runtime.List:
		if len(value.Elements) != 0 {
			return nil, fmt.Errorf("unsupported bytecode constant: non-empty list literal")
		}
		return append(out, 11), nil
	case runtime.Set:
		if len(value.Elements) != 0 {
			return nil, fmt.Errorf("unsupported bytecode constant: non-empty set literal")
		}
		return append(out, 12), nil
	default:
		return nil, fmt.Errorf("unsupported bytecode constant %s", value.TypeName())
	}
}

func appendString(out []byte, value string) []byte {
	out = binary.BigEndian.AppendUint16(out, uint16(len(value)))
	return append(out, []byte(value)...)
}

func appendFunctionMetadata(out []byte, metadata *runtime.FunctionMetadata) ([]byte, error) {
	if metadata == nil {
		return append(out, 0), nil
	}
	out = append(out, 1)
	out = appendString(out, metadata.Name)
	out = appendString(out, metadata.Target)
	out = appendString(out, metadata.Doc)
	out = appendString(out, metadata.ReturnType)
	out = binary.BigEndian.AppendUint16(out, uint16(len(metadata.TypeParameters)))
	for _, name := range metadata.TypeParameters {
		out = appendString(out, name)
	}
	if metadata.Async {
		out = append(out, 1)
	} else {
		out = append(out, 0)
	}
	if metadata.Variadic {
		out = append(out, 1)
	} else {
		out = append(out, 0)
	}
	out = binary.BigEndian.AppendUint16(out, uint16(len(metadata.Parameters)))
	for _, parameter := range metadata.Parameters {
		out = appendString(out, parameter.Name)
		out = appendString(out, parameter.Type)
		if parameter.Variadic {
			out = append(out, 1)
		} else {
			out = append(out, 0)
		}
		if parameter.HasDefault {
			out = append(out, 1)
		} else {
			out = append(out, 0)
		}
		var err error
		out, err = appendDecoratorMetadata(out, parameter.Decorators)
		if err != nil {
			return nil, err
		}
	}
	out, err := appendDecoratorMetadata(out, metadata.Decorators)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func appendClassMetadata(out []byte, metadata *runtime.ClassMetadata) []byte {
	if metadata == nil {
		return append(out, 0)
	}
	out = append(out, 1)
	out = appendString(out, metadata.Name)
	out = appendString(out, metadata.Doc)
	out = appendString(out, metadata.Parent)
	out = appendStringList(out, metadata.Fields)
	out = appendStringList(out, metadata.Methods)
	out = appendStringList(out, metadata.StaticMethods)
	out = appendStringList(out, metadata.Interfaces)
	return out
}

func appendStringList(out []byte, values []string) []byte {
	out = binary.BigEndian.AppendUint16(out, uint16(len(values)))
	for _, value := range values {
		out = appendString(out, value)
	}
	return out
}

func appendDecoratorMetadata(out []byte, decorators []runtime.DecoratorMetadata) ([]byte, error) {
	out = binary.BigEndian.AppendUint16(out, uint16(len(decorators)))
	for _, decorator := range decorators {
		out = appendString(out, decorator.Name)
		out = appendString(out, decorator.Target)
		out = binary.BigEndian.AppendUint64(out, uint64(decorator.Position))
		out = binary.BigEndian.AppendUint64(out, uint64(decorator.Overload))
		out = binary.BigEndian.AppendUint64(out, uint64(decorator.Line))
		out = binary.BigEndian.AppendUint64(out, uint64(decorator.Column))
		out = binary.BigEndian.AppendUint16(out, uint16(len(decorator.Args)))
		for _, arg := range decorator.Args {
			var err error
			out, err = appendMetadataValue(out, arg)
			if err != nil {
				return nil, err
			}
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(decorator.NamedArgs)))
		for name, arg := range decorator.NamedArgs {
			out = appendString(out, name)
			var err error
			out, err = appendMetadataValue(out, arg)
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func appendDecoratorMetadataMap(out []byte, items map[string][]runtime.DecoratorMetadata) ([]byte, error) {
	out = binary.BigEndian.AppendUint16(out, uint16(len(items)))
	for name, decorators := range items {
		out = appendString(out, name)
		var err error
		out, err = appendDecoratorMetadata(out, decorators)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func appendMetadataValue(out []byte, value runtime.Value) ([]byte, error) {
	switch value := value.(type) {
	case runtime.Null, runtime.Bool, runtime.SmallInt, runtime.Int, runtime.String, runtime.Decimal, runtime.Float:
		return appendConstant(out, value)
	case *runtime.List:
		out = append(out, 7)
		out = binary.BigEndian.AppendUint16(out, uint16(len(value.Elements)))
		for _, element := range value.Elements {
			var err error
			out, err = appendMetadataValue(out, element)
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	case runtime.Dict:
		out = append(out, 8)
		out = binary.BigEndian.AppendUint16(out, uint16(value.Len()))
		var dictErr error
		value.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			var err error
			out, err = appendMetadataValue(out, entry.Key)
			if err != nil {
				dictErr = err
				return false
			}
			out, err = appendMetadataValue(out, entry.Value)
			if err != nil {
				dictErr = err
				return false
			}
			return true
		})
		if dictErr != nil {
			return nil, dictErr
		}
		return out, nil
	case runtime.Set:
		out = append(out, 9)
		out = binary.BigEndian.AppendUint16(out, uint16(len(value.Elements)))
		for _, entry := range value.Elements {
			var err error
			out, err = appendMetadataValue(out, entry.Value)
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported decorator metadata constant %s", value.TypeName())
	}
}

type byteReader struct {
	data []byte
	pos  int
	err  error
}

func (r *byteReader) read(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.pos+n > len(r.data) {
		r.err = errors.New("truncated bytecode")
		return nil
	}
	value := r.data[r.pos : r.pos+n]
	r.pos += n
	return value
}

func (r *byteReader) u8() uint8 {
	data := r.read(1)
	if r.err != nil {
		return 0
	}
	return data[0]
}

func (r *byteReader) u16() uint16 {
	data := r.read(2)
	if r.err != nil {
		return 0
	}
	return binary.BigEndian.Uint16(data)
}

func (r *byteReader) u32() uint32 {
	data := r.read(4)
	if r.err != nil {
		return 0
	}
	return binary.BigEndian.Uint32(data)
}

func (r *byteReader) u64() uint64 {
	data := r.read(8)
	if r.err != nil {
		return 0
	}
	return binary.BigEndian.Uint64(data)
}

func (r *byteReader) constant() runtime.Value {
	tag := r.u8()
	switch tag {
	case 0:
		return runtime.Null{}
	case 1:
		return runtime.Bool{Value: r.u8() != 0}
	case 2:
		text := string(r.read(int(r.u32())))
		value, err := runtime.NewIntLiteral(text)
		if err != nil && r.err == nil {
			r.err = err
		}
		if value.Value.IsInt64() {
			return runtime.SmallInt{Value: value.Value.Int64()}
		}
		return value
	case 3:
		return runtime.String{Value: string(r.read(int(r.u32())))}
	case 4:
		text := string(r.read(int(r.u32())))
		value, err := runtime.NewDecimalLiteral(text)
		if err != nil && r.err == nil {
			r.err = err
		}
		return value
	case 5:
		return runtime.Float{Value: math.Float64frombits(r.u64())}
	case 6:
		target := runtime.DecoratorTarget{Target: r.string()}
		target.Function = r.functionMetadata()
		target.Class = r.classMetadata()
		count := int(r.u16())
		target.Decorators = make([]runtime.DecoratorMetadata, 0, count)
		for i := 0; i < count; i++ {
			decorator := runtime.DecoratorMetadata{
				Name:      r.string(),
				Target:    r.string(),
				Position:  int64(r.u64()),
				Overload:  int64(r.u64()),
				Line:      int64(r.u64()),
				Column:    int64(r.u64()),
				NamedArgs: map[string]runtime.Value{},
			}
			argCount := int(r.u16())
			decorator.Args = make([]runtime.Value, 0, argCount)
			for j := 0; j < argCount; j++ {
				decorator.Args = append(decorator.Args, r.metadataValue())
			}
			namedCount := int(r.u16())
			for j := 0; j < namedCount; j++ {
				decorator.NamedArgs[r.string()] = r.metadataValue()
			}
			target.Decorators = append(target.Decorators, decorator)
		}
		return target
	case 7:
		name := r.string()
		module := r.string()
		backingType := r.string()
		count := int(r.u16())
		enum := &runtime.EnumDef{Name: name, Module: module, BackingType: backingType, Variants: make([]runtime.EnumVariantDefRuntime, 0, count)}
		for i := 0; i < count; i++ {
			variantName := r.string()
			fieldCount := int(r.u16())
			var backingValue runtime.Value
			if r.u8() != 0 {
				backingValue = r.metadataValue()
			}
			enum.Variants = append(enum.Variants, runtime.EnumVariantDefRuntime{Name: variantName, FieldCount: fieldCount, BackingValue: backingValue})
		}
		implementsCount := int(r.u16())
		for i := 0; i < implementsCount; i++ {
			enum.Implements = append(enum.Implements, r.string())
		}
		methodCount := int(r.u16())
		if methodCount > 0 {
			enum.MethodIndices = make(map[string][]int64, methodCount)
			for i := 0; i < methodCount; i++ {
				methodName := r.string()
				indexCount := int(r.u16())
				indices := make([]int64, 0, indexCount)
				for j := 0; j < indexCount; j++ {
					indices = append(indices, int64(r.u64()))
				}
				enum.MethodIndices[methodName] = indices
			}
		}
		return enum
	case 8:
		return runtime.Type{Name: r.string()}
	case 9:
		return runtime.BytecodeClass{
			Name:             r.string(),
			Doc:              r.string(),
			Index:            int64(r.u64()),
			Decorators:       r.decoratorMetadata(),
			MethodDecorators: r.decoratorMetadataMap(),
			StaticDecorators: r.decoratorMetadataMap(),
		}
	case 10:
		return runtime.Dict{Entries: map[string]runtime.DictEntry{}}
	case 11:
		return &runtime.List{Elements: nil}
	case 12:
		return runtime.Set{Elements: map[string]runtime.SetEntry{}}
	default:
		if r.err == nil {
			r.err = fmt.Errorf("unknown constant tag %d", tag)
		}
		return runtime.Null{}
	}
}

func (r *byteReader) string() string {
	return string(r.read(int(r.u16())))
}

func (r *byteReader) functionMetadata() *runtime.FunctionMetadata {
	if r.u8() == 0 {
		return nil
	}
	metadata := runtime.FunctionMetadata{
		Name:       r.string(),
		Target:     r.string(),
		Doc:        r.string(),
		ReturnType: r.string(),
	}
	typeParamCount := int(r.u16())
	metadata.TypeParameters = make([]string, 0, typeParamCount)
	for i := 0; i < typeParamCount; i++ {
		metadata.TypeParameters = append(metadata.TypeParameters, r.string())
	}
	metadata.Async = r.u8() != 0
	metadata.Variadic = r.u8() != 0
	paramCount := int(r.u16())
	metadata.Parameters = make([]runtime.ParameterMetadata, 0, paramCount)
	for i := 0; i < paramCount; i++ {
		param := runtime.ParameterMetadata{
			Name:       r.string(),
			Type:       r.string(),
			Variadic:   r.u8() != 0,
			HasDefault: r.u8() != 0,
		}
		param.Decorators = r.decoratorMetadata()
		metadata.Parameters = append(metadata.Parameters, param)
	}
	metadata.Decorators = r.decoratorMetadata()
	return &metadata
}

func (r *byteReader) classMetadata() *runtime.ClassMetadata {
	if r.u8() == 0 {
		return nil
	}
	return &runtime.ClassMetadata{
		Name:          r.string(),
		Doc:           r.string(),
		Parent:        r.string(),
		Fields:        r.stringList(),
		Methods:       r.stringList(),
		StaticMethods: r.stringList(),
		Interfaces:    r.stringList(),
	}
}

func (r *byteReader) stringList() []string {
	count := int(r.u16())
	values := make([]string, 0, count)
	for i := 0; i < count; i++ {
		values = append(values, r.string())
	}
	return values
}

func (r *byteReader) decoratorMetadata() []runtime.DecoratorMetadata {
	count := int(r.u16())
	decorators := make([]runtime.DecoratorMetadata, 0, count)
	for i := 0; i < count; i++ {
		decorator := runtime.DecoratorMetadata{
			Name:      r.string(),
			Target:    r.string(),
			Position:  int64(r.u64()),
			Overload:  int64(r.u64()),
			Line:      int64(r.u64()),
			Column:    int64(r.u64()),
			NamedArgs: map[string]runtime.Value{},
		}
		argCount := int(r.u16())
		decorator.Args = make([]runtime.Value, 0, argCount)
		for j := 0; j < argCount; j++ {
			decorator.Args = append(decorator.Args, r.metadataValue())
		}
		namedCount := int(r.u16())
		for j := 0; j < namedCount; j++ {
			decorator.NamedArgs[r.string()] = r.metadataValue()
		}
		decorators = append(decorators, decorator)
	}
	return decorators
}

func (r *byteReader) decoratorMetadataMap() map[string][]runtime.DecoratorMetadata {
	count := int(r.u16())
	items := map[string][]runtime.DecoratorMetadata{}
	for i := 0; i < count; i++ {
		items[r.string()] = r.decoratorMetadata()
	}
	return items
}

func (r *byteReader) metadataValue() runtime.Value {
	tag := r.u8()
	switch tag {
	case 0:
		return runtime.Null{}
	case 1:
		return runtime.Bool{Value: r.u8() != 0}
	case 2:
		text := string(r.read(int(r.u32())))
		value, err := runtime.NewIntLiteral(text)
		if err != nil && r.err == nil {
			r.err = err
		}
		if value.Value.IsInt64() {
			return runtime.SmallInt{Value: value.Value.Int64()}
		}
		return value
	case 3:
		return runtime.String{Value: string(r.read(int(r.u32())))}
	case 4:
		text := string(r.read(int(r.u32())))
		value, err := runtime.NewDecimalLiteral(text)
		if err != nil && r.err == nil {
			r.err = err
		}
		return value
	case 5:
		return runtime.Float{Value: math.Float64frombits(r.u64())}
	case 7:
		count := int(r.u16())
		values := make([]runtime.Value, 0, count)
		for i := 0; i < count; i++ {
			values = append(values, r.metadataValue())
		}
		return &runtime.List{Elements: values}
	case 8:
		count := int(r.u16())
		entries := map[string]runtime.DictEntry{}
		for i := 0; i < count; i++ {
			key := r.metadataValue()
			value := r.metadataValue()
			entries[native.DictKey(key)] = runtime.DictEntry{Key: key, Value: value}
		}
		return runtime.Dict{Entries: entries}
	case 9:
		count := int(r.u16())
		entries := map[string]runtime.SetEntry{}
		for i := 0; i < count; i++ {
			value := r.metadataValue()
			entries[native.DictKey(value)] = runtime.SetEntry{Value: value}
		}
		return runtime.Set{Elements: entries}
	default:
		if r.err == nil {
			r.err = fmt.Errorf("unknown decorator metadata tag %d", tag)
		}
		return runtime.Null{}
	}
}

// MethodParamNames resolves a class method's declared parameter names
// (receiver slot dropped), walking same-chunk parents. The module
// loader uses it so dict spread can order named args for a
// cross-module method call.
func (c *Chunk) MethodParamNames(className, methodName string) ([]string, error) {
	lowerClass := strings.ToLower(className)
	for i := range c.Classes {
		if strings.ToLower(c.Classes[i].Name) != lowerClass {
			continue
		}
		info := c.Classes[i]
		lowered := strings.ToLower(methodName)
		for {
			if indices, ok := info.Methods[lowered]; ok {
				if len(indices) != 1 {
					return nil, fmt.Errorf("cannot use dict spread with overloaded method %s.%s", className, methodName)
				}
				fn := c.Functions[indices[0]]
				if len(fn.ParamNames) > 0 {
					return fn.ParamNames[1:], nil
				}
				return nil, nil
			}
			if info.ParentIndex >= 0 && int(info.ParentIndex) < len(c.Classes) {
				info = c.Classes[info.ParentIndex]
				continue
			}
			return nil, fmt.Errorf("unknown method %s.%s", className, methodName)
		}
	}
	return nil, fmt.Errorf("unknown class %s", className)
}

// ConstructorParamNames returns the union of a class's own constructor overloads' parameter names (receiver slot dropped) for dict-spread key dropping; constructors are not inherited, so parents are not walked.
func (c *Chunk) ConstructorParamNames(className string) ([]string, error) {
	lowerClass := strings.ToLower(className)
	for i := range c.Classes {
		if strings.ToLower(c.Classes[i].Name) != lowerClass {
			continue
		}
		seen := map[string]bool{}
		names := []string{}
		for _, idx := range c.Classes[i].ConstructorIndices {
			if idx < 0 || int(idx) >= len(c.Functions) {
				continue
			}
			params := c.Functions[idx].ParamNames
			if len(params) > 0 {
				params = params[1:]
			}
			for _, name := range params {
				if !seen[name] {
					seen[name] = true
					names = append(names, name)
				}
			}
		}
		return names, nil
	}
	return nil, fmt.Errorf("unknown class %s", className)
}

// FunctionParamNames returns a free function's declared parameter names, so the loader can order named args for a cross-module function call.
func (c *Chunk) FunctionParamNames(index int64) ([]string, error) {
	if index < 0 || int(index) >= len(c.Functions) {
		return nil, fmt.Errorf("function index out of range")
	}
	return c.Functions[index].ParamNames, nil
}
