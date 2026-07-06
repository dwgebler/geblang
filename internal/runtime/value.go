package runtime

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"

	"geblang/internal/ast"
)

// ErrTaskCancelled is the sentinel error stored on a Task that was
// cancelled before completing. Surfaced through Await for any caller
// that holds the task value.
var ErrTaskCancelled = errors.New("task cancelled")

type Value interface {
	TypeName() string
	Inspect() string
}

type Null struct{}

func (Null) TypeName() string { return "null" }
func (Null) Inspect() string  { return "null" }

type Bool struct {
	Value bool
}

func (v Bool) TypeName() string { return "bool" }
func (v Bool) Inspect() string {
	if v.Value {
		return "true"
	}
	return "false"
}

type Int struct {
	Value *big.Int
}

func NewIntLiteral(lit string) (Int, error) {
	value, err := ast.ParseIntLiteral(lit)
	if err != nil {
		return Int{}, err
	}
	return Int{Value: value}, nil
}

func NewInt64(v int64) Int {
	return Int{Value: big.NewInt(v)}
}

func (v Int) TypeName() string { return "int" }
func (v Int) Inspect() string  { return v.Value.String() }

// SmallInt is an int that fits in int64. It is stored without heap allocation
// when boxed into the Value interface, making it the fast path for integer
// arithmetic. TypeName returns "int" (same as Int) so type checks are uniform.
type SmallInt struct {
	Value int64
}

func (v SmallInt) TypeName() string { return "int" }
func (v SmallInt) Inspect() string  { return strconv.FormatInt(v.Value, 10) }

type Decimal struct {
	Value *big.Rat
}

func NewDecimalLiteral(lit string) (Decimal, error) {
	value, err := ast.ParseDecimalLiteral(lit)
	if err != nil {
		return Decimal{}, err
	}
	return Decimal{Value: value}, nil
}

func (v Decimal) TypeName() string { return "decimal" }
func (v Decimal) Inspect() string  { return v.Value.FloatString(10) }

type Float struct {
	Value float64
}

func (v Float) TypeName() string { return "float" }
func (v Float) Inspect() string  { return fmt.Sprintf("%g", v.Value) }

type String struct {
	Value string
}

func (v String) TypeName() string { return "string" }
func (v String) Inspect() string  { return v.Value }

// StringAccumulator masquerades as a string in a slot but defers
// allocation: getLocalVM / getGlobalVM materialise it to a String on
// read. Lives only in slots written by OpAppendStringConstStmt.
type StringAccumulator struct {
	B *strings.Builder
}

func (v *StringAccumulator) TypeName() string { return "string" }
func (v *StringAccumulator) Inspect() string {
	if v == nil || v.B == nil {
		return ""
	}
	return v.B.String()
}

// Materialize returns the accumulator's current contents as a
// runtime.String. Safe to call when the underlying builder is nil.
func (v *StringAccumulator) Materialize() String {
	if v == nil || v.B == nil {
		return String{Value: ""}
	}
	return String{Value: v.B.String()}
}

type Bytes struct {
	Value []byte
}

func (v Bytes) TypeName() string { return "bytes" }
func (v Bytes) Inspect() string  { return hex.EncodeToString(v.Value) }

type DateTimeInstant struct {
	Unix int64
}

func (v DateTimeInstant) TypeName() string { return "datetime.Instant" }
func (v DateTimeInstant) Inspect() string  { return fmt.Sprintf("<datetime.Instant %d>", v.Unix) }

type DateTimeDuration struct {
	Seconds int64
}

func (v DateTimeDuration) TypeName() string { return "datetime.Duration" }
func (v DateTimeDuration) Inspect() string  { return fmt.Sprintf("<datetime.Duration %ds>", v.Seconds) }

type DateTimeZone struct {
	Name string
}

func (v DateTimeZone) TypeName() string { return "datetime.Zone" }
func (v DateTimeZone) Inspect() string  { return "<datetime.Zone " + v.Name + ">" }

type URLValue struct {
	Raw string
}

func (v URLValue) TypeName() string { return "url.URL" }
func (v URLValue) Inspect() string  { return "<url.URL " + v.Raw + ">" }

type HTTPHeaders struct {
	Values map[string][]string
}

func (v HTTPHeaders) TypeName() string { return "http.Headers" }
func (v HTTPHeaders) Inspect() string  { return "<http.Headers>" }

type HTTPCookie struct {
	Name     string
	Value    string
	Path     string
	Domain   string
	Expires  int64
	MaxAge   int64
	Secure   bool
	HTTPOnly bool
	SameSite string
}

func (v HTTPCookie) TypeName() string { return "http.Cookie" }
func (v HTTPCookie) Inspect() string  { return "<http.Cookie " + v.Name + ">" }

type TemplateValue struct {
	Name string
	Text string
	Path string
}

func (v TemplateValue) TypeName() string { return "template.Template" }
func (v TemplateValue) Inspect() string {
	if v.Name != "" {
		return "<template.Template " + v.Name + ">"
	}
	return "<template.Template>"
}

type TemplateEngine struct {
	Dir string
}

func (v TemplateEngine) TypeName() string { return "template.Engine" }
func (v TemplateEngine) Inspect() string  { return "<template.Engine " + v.Dir + ">" }

type List struct {
	Elements []Value
	Frozen   bool
	// ElementTypes is the optional declared element-type tag attached
	// when the list flows through a typed-declaration boundary
	// (`list<int> xs = ...`). nil for untagged lists. Mutating methods
	// propagate the tag through; reflect.typeBindings reads it.
	ElementTypes []string
}

func (v List) TypeName() string { return "list" }
func (v List) Inspect() string {
	parts := make([]string, 0, len(v.Elements))
	for _, el := range v.Elements {
		parts = append(parts, inspectInsideContainer(el, 0))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

type Dict struct {
	// data is the inline-capable shared store for dicts built through
	// NewDict / NewDictHint. When nil, the dict is a literal-built
	// map-state dict using Entries / Order directly. Accessors check
	// data first; see dict_store.go.
	data *dictData
	// Entries / Order back literal-constructed dicts (Dict{Entries:...}).
	Entries map[string]DictEntry
	Order   *[]string
	Frozen  bool
	// ElementTypes mirrors List.ElementTypes for dicts. When set, the
	// slice has two entries: [keyType, valueType] for `dict<K, V>`.
	ElementTypes []string
}

func NewDict() Dict {
	return Dict{data: &dictData{}}
}

// NewDictHint sizes the store for n entries: dicts that stay at or
// below dictInlineMax keep their entries inline (one allocation, no
// map); larger ones start in the spilled map form.
func NewDictHint(n int) Dict {
	d := &dictData{}
	if n > dictInlineMax {
		d.m = make(map[string]DictEntry, n)
		d.order = make([]string, 0, n)
	}
	return Dict{data: d}
}

func (v Dict) PutEntry(key string, entry DictEntry) {
	if v.data != nil {
		v.data.set(key, entry)
		return
	}
	if v.Entries == nil {
		return
	}
	if _, hit := v.Entries[key]; !hit && v.Order != nil {
		*v.Order = append(*v.Order, key)
	}
	v.Entries[key] = entry
}

func (v Dict) DelEntry(key string) {
	if v.data != nil {
		v.data.del(key)
		return
	}
	if v.Entries == nil {
		return
	}
	if _, hit := v.Entries[key]; !hit {
		return
	}
	delete(v.Entries, key)
	if v.Order == nil {
		return
	}
	for i, k := range *v.Order {
		if k == key {
			*v.Order = append((*v.Order)[:i], (*v.Order)[i+1:]...)
			return
		}
	}
}

// Clear removes all entries.
func (v Dict) Clear() {
	if v.data != nil {
		v.data.clear()
		return
	}
	if v.Entries != nil {
		for k := range v.Entries {
			delete(v.Entries, k)
		}
	}
	if v.Order != nil {
		*v.Order = (*v.Order)[:0]
	}
}

// GetEntry looks up an entry by its DictKey string. The accessor
// (with PutEntry / DelEntry / Len / ForEachEntry / EntryKeys) is the
// stable surface that lets the storage representation change without
// touching consumers.
func (v Dict) GetEntry(key string) (DictEntry, bool) {
	if v.data != nil {
		return v.data.get(key)
	}
	if v.Entries == nil {
		return DictEntry{}, false
	}
	e, ok := v.Entries[key]
	return e, ok
}

// Len reports the number of entries.
func (v Dict) Len() int {
	if v.data != nil {
		return v.data.length()
	}
	return len(v.Entries)
}

// EntryValue returns the value for key, or nil when absent. For
// call sites that index then take .Value and assume presence.
func (v Dict) EntryValue(key string) Value {
	if v.data != nil {
		e, _ := v.data.get(key)
		return e.Value
	}
	if v.Entries == nil {
		return nil
	}
	return v.Entries[key].Value
}

// ForEachEntry visits entries in insertion order. The callback returns
// false to stop early. Order falls back to sorted keys when the Order
// list is unpopulated (legacy construction paths), matching OrderedKeys.
func (v Dict) ForEachEntry(fn func(key string, entry DictEntry) bool) {
	if v.data != nil {
		v.data.forEach(fn)
		return
	}
	if v.Entries == nil {
		return
	}
	if v.Order != nil && len(*v.Order) == len(v.Entries) {
		for _, k := range *v.Order {
			if e, ok := v.Entries[k]; ok {
				if !fn(k, e) {
					return
				}
			}
		}
		return
	}
	for _, k := range v.OrderedKeys() {
		if !fn(k, v.Entries[k]) {
			return
		}
	}
}

// EntryKeys returns a fresh slice of DictKey strings in insertion
// order. Use ForEachEntry for hot iteration; EntryKeys suits loops
// whose control flow (break to an outer scope, error returns) does
// not fit a callback.
func (v Dict) EntryKeys() []string { return v.OrderedKeys() }

// OrderedKeys returns keys in insertion order; falls back to a sort
// when Order is unpopulated (legacy construction paths).
func (v Dict) OrderedKeys() []string {
	if v.data != nil {
		return v.data.orderedKeys()
	}
	if v.Order != nil && len(*v.Order) == len(v.Entries) {
		out := make([]string, len(*v.Order))
		copy(out, *v.Order)
		return out
	}
	keys := make([]string, 0, len(v.Entries))
	for k := range v.Entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (v Dict) TypeName() string { return "dict" }
func (v Dict) Inspect() string {
	keys := v.OrderedKeys()
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		entry, _ := v.GetEntry(k)
		kStr := inspectInsideContainer(entry.Key, 0)
		vStr := inspectInsideContainer(entry.Value, 0)
		parts = append(parts, kStr+": "+vStr)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

type DictEntry struct {
	Key   Value
	Value Value
}

type Set struct {
	Elements map[string]SetEntry
	Frozen   bool
	// ElementTypes mirrors List.ElementTypes for sets. When set, the
	// slice has one entry: [elementType] for `set<T>`.
	ElementTypes []string
}

func (v Set) TypeName() string { return "set" }
func (v Set) Inspect() string {
	parts := make([]string, 0, len(v.Elements))
	for _, entry := range v.Elements {
		parts = append(parts, inspectInsideContainer(entry.Value, 0))
	}
	sort.Strings(parts)
	return "set{" + strings.Join(parts, ", ") + "}"
}

const maxInspectDepth = 30

func inspectInsideContainer(v Value, depth int) string {
	if depth > maxInspectDepth {
		return `"<cycle>"`
	}
	switch x := v.(type) {
	case String:
		return jsonQuoteString(x.Value)
	case *List:
		parts := make([]string, 0, len(x.Elements))
		for _, el := range x.Elements {
			parts = append(parts, inspectInsideContainer(el, depth+1))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case Dict:
		keys := x.OrderedKeys()
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			entry, _ := x.GetEntry(k)
			kStr := inspectInsideContainer(entry.Key, depth+1)
			vStr := inspectInsideContainer(entry.Value, depth+1)
			parts = append(parts, kStr+": "+vStr)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case Set:
		parts := make([]string, 0, len(x.Elements))
		for _, entry := range x.Elements {
			parts = append(parts, inspectInsideContainer(entry.Value, depth+1))
		}
		sort.Strings(parts)
		return "set{" + strings.Join(parts, ", ") + "}"
	}
	return v.Inspect()
}

func jsonQuoteString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

type SetEntry struct {
	Value Value
}

// DictPairs returns insertion-ordered [key, value] pairs, the canonical
// dict iteration view on both backends.
func DictPairs(d Dict) []Value {
	values := make([]Value, 0, d.Len())
	d.ForEachEntry(func(_ string, entry DictEntry) bool {
		values = append(values, &List{Elements: []Value{entry.Key, entry.Value}})
		return true
	})
	return values
}

// StringChars returns per-rune single-character strings, matching
// string.chars(); the canonical string iteration view on both backends.
func StringChars(s String) []Value {
	runes := []rune(s.Value)
	values := make([]Value, len(runes))
	for i, r := range runes {
		values[i] = String{Value: string(r)}
	}
	return values
}

type Range struct {
	Start     *big.Int
	End       *big.Int
	Exclusive bool
	Step      *big.Int
}

func (v Range) TypeName() string { return "range" }
func (v Range) Inspect() string {
	var sb strings.Builder
	sb.WriteString(v.Start.String())
	sb.WriteString("..")
	if v.Exclusive {
		sb.WriteByte('<')
	}
	sb.WriteString(v.End.String())
	if v.Step != nil && v.Step.Cmp(big.NewInt(1)) != 0 {
		sb.WriteString(" by ")
		sb.WriteString(v.Step.String())
	}
	return sb.String()
}

// Length returns the number of elements produced by iterating this range.
func (v Range) Length() *big.Int {
	step := v.Step
	if step.Sign() == 0 {
		return new(big.Int)
	}
	diff := new(big.Int).Sub(v.End, v.Start)
	if step.Sign() > 0 {
		if diff.Sign() < 0 {
			return new(big.Int)
		}
	} else {
		if diff.Sign() > 0 {
			return new(big.Int)
		}
		diff.Neg(diff)
		step = new(big.Int).Neg(step)
	}
	if v.Exclusive {
		count := new(big.Int).Add(diff, new(big.Int).Sub(step, big.NewInt(1)))
		return count.Div(count, step)
	}
	count := new(big.Int).Div(diff, step)
	return count.Add(count, big.NewInt(1))
}

// ContainsInt reports whether n is a value produced by iterating this range.
func (v Range) ContainsInt(n *big.Int) bool {
	step := v.Step
	if step.Sign() == 0 {
		return false
	}
	if step.Sign() > 0 {
		if n.Cmp(v.Start) < 0 {
			return false
		}
		if v.Exclusive && n.Cmp(v.End) >= 0 {
			return false
		}
		if !v.Exclusive && n.Cmp(v.End) > 0 {
			return false
		}
	} else {
		if n.Cmp(v.Start) > 0 {
			return false
		}
		if v.Exclusive && n.Cmp(v.End) <= 0 {
			return false
		}
		if !v.Exclusive && n.Cmp(v.End) < 0 {
			return false
		}
	}
	diff := new(big.Int).Sub(n, v.Start)
	rem := new(big.Int).Rem(diff, step)
	return rem.Sign() == 0
}

type Generator struct {
	mu       sync.Mutex
	elements []Value
	index    int
	next     func() (Value, bool, error)
	close    func()
	closed   bool
	// prefetch backs done(): it peeks one item ahead; Next consumes it first.
	prefetched  Value
	hasPrefetch bool
}

func NewGeneratorFromSlice(elements []Value) *Generator {
	return &Generator{elements: append([]Value(nil), elements...)}
}

func NewGenerator(next func() (Value, bool, error)) *Generator {
	return &Generator{next: next}
}

func NewClosableGenerator(next func() (Value, bool, error), close func()) *Generator {
	return &Generator{next: next, close: close}
}

func (v *Generator) TypeName() string { return "generator" }
func (v *Generator) Inspect() string  { return "<generator>" }
func (v *Generator) Next() (Value, bool, error) {
	if v == nil {
		return nil, false, nil
	}
	v.mu.Lock()
	if v.hasPrefetch {
		value := v.prefetched
		v.prefetched = nil
		v.hasPrefetch = false
		v.mu.Unlock()
		return value, true, nil
	}
	if v.closed {
		v.mu.Unlock()
		return nil, false, nil
	}
	v.mu.Unlock()
	if v.next != nil {
		return v.next()
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.index >= len(v.elements) {
		return nil, false, nil
	}
	value := v.elements[v.index]
	v.index++
	return value, true, nil
}

// PeekDone reports exhaustion by prefetching one item; Next consumes
// the prefetched item first, so done() never loses a value.
func (v *Generator) PeekDone() (bool, error) {
	v.mu.Lock()
	if v.hasPrefetch {
		v.mu.Unlock()
		return false, nil
	}
	v.mu.Unlock()
	value, ok, err := v.Next()
	if err != nil {
		return false, err
	}
	if !ok {
		return true, nil
	}
	v.mu.Lock()
	v.prefetched = value
	v.hasPrefetch = true
	v.mu.Unlock()
	return false, nil
}

func (v *Generator) Close() {
	if v == nil {
		return
	}
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return
	}
	v.closed = true
	closeFn := v.close
	v.mu.Unlock()
	if closeFn != nil {
		closeFn()
	}
}

type Function struct {
	Name                 string
	Doc                  string
	TypeParameters       []string
	TypeParamConstraints map[string]*ast.TypeRef
	Parameters           []ast.Parameter
	ReturnType           *ast.TypeRef
	Body                 *ast.BlockStatement
	Env                  *Environment
	Decorators           []ast.Decorator
	Target               string
	Async                bool
	IsGenerator          bool
	Native               func(this *Instance, args []Value) (Value, error)
	ForwardThis          bool
	// OriginalParameters: pre-decoration signature, used only for named-arg binding.
	OriginalParameters []ast.Parameter
	// OwnerClass is set when the function is a class method/constructor; it
	// identifies the lexical class for `parent(...)` resolution, which must
	// dispatch on the declaring class rather than the runtime instance class
	// (otherwise `parent` inside a parent's body would re-enter that same
	// parent through the subclass's instance).
	OwnerClass *Class
	// TypeBindings captures the enclosing generic function's type
	// parameter bindings at the moment this function value was created.
	// Anonymous functions (lambdas) and references to generic top-level
	// functions both pick this up so that an inner `T x` parameter
	// resolves against the outer call site's concrete bindings.
	// Mirror of BytecodeClosure.TypeBindings on the VM side.
	TypeBindings map[string]string
	// DefinitionModule / DefinitionLine / DefinitionColumn capture the
	// source position of the function declaration, surfaced by
	// reflect.location.
	DefinitionModule string
	DefinitionLine   int
	DefinitionColumn int
	// BridgeInvoke (host typed any to avoid a runtime->bytecode import cycle) runs a bridged callback threading the invoking worker as a synchronous re-entry host; nil for non-bridged functions.
	BridgeInvoke func(host any, args []Value) (Value, error)
}

func (v Function) TypeName() string { return "func" }
func (v Function) Inspect() string {
	if v.Name != "" {
		return "<func " + v.Name + ">"
	}
	return "<func>"
}

type OverloadedFunction struct {
	Name      string
	Overloads []Function
	// Set for an exported bytecode overload set; a cross-module member call selects a home-chunk overload by Indices.
	Module  string
	Indices []int64
}

func (v OverloadedFunction) TypeName() string { return "func" }
func (v OverloadedFunction) Inspect() string {
	if v.Name != "" {
		return "<overloaded func " + v.Name + ">"
	}
	return "<overloaded func>"
}

type DecoratorMetadata struct {
	Name      string
	Target    string
	Position  int64
	Overload  int64
	Args      []Value
	NamedArgs map[string]Value
	Line      int64
	Column    int64
}

type ParameterMetadata struct {
	Name       string
	Type       string
	Variadic   bool
	HasDefault bool
	Decorators []DecoratorMetadata
}

type FunctionMetadata struct {
	Name           string
	Target         string
	Doc            string
	TypeParameters []string
	Parameters     []ParameterMetadata
	ReturnType     string
	Async          bool
	Variadic       bool
	Decorators     []DecoratorMetadata
	// Module / DefLine / DefColumn capture the source position of
	// the function's `func` keyword. Surfaced by reflect.location.
	// Empty / zero when the function did not originate from a
	// bytecode chunk (e.g. native stdlib functions).
	Module    string
	DefLine   int64
	DefColumn int64
}

type ClassMetadata struct {
	Name          string
	Doc           string
	Parent        string
	Fields        []string
	Methods       []string
	StaticMethods []string
	Interfaces    []string
	// Module / DefLine / DefColumn capture the source position of
	// the class's `class` keyword. Surfaced by reflect.location.
	Module    string
	DefLine   int64
	DefColumn int64
}

type DecoratorTarget struct {
	Target     string
	Decorators []DecoratorMetadata
	Function   *FunctionMetadata
	Class      *ClassMetadata
	Callable   Value
}

func (v DecoratorTarget) TypeName() string { return "reflectTarget" }
func (v DecoratorTarget) Inspect() string  { return "<reflect " + v.Target + ">" }

type Module struct {
	Name      string
	Canonical string
	Exports   map[string]Value
}

func (v *Module) TypeName() string { return "module" }
func (v *Module) Inspect() string  { return "<module " + v.Name + ">" }

type BytecodeFunction struct {
	Module         string
	Name           string
	Doc            string
	TypeParameters []string
	Index          int64
	Raw            bool
	Parameters     []ParameterMetadata
	ReturnType     string
	Async          bool
	Variadic       bool
	Decorators     []DecoratorMetadata
	// DefLine / DefColumn capture the source position of the
	// function declaration, surfaced by reflect.location.
	DefLine   int64
	DefColumn int64
}

func (v BytecodeFunction) TypeName() string { return "function" }
func (v BytecodeFunction) Inspect() string {
	if v.Module == "" {
		return "<bytecode func " + v.Name + ">"
	}
	return "<bytecode func " + v.Module + "." + v.Name + ">"
}

type BytecodeClosure struct {
	FunctionIndex int64
	Name          string
	Module        string
	Upvalues      []Value
	// TypeBindings captures the enclosing generic function's type
	// parameter bindings at the moment the closure was created. When
	// the closure later runs, these bindings are planted into its call
	// frame so that an anonymous function declared inside `func f<T>(...)`
	// can still reference `T` as a parameter type or in `instanceof T`
	// checks. A named generic function passed by reference (compiled
	// to OpMakeClosure with zero upvalues) is treated symmetrically.
	// Nil when the closure was created outside any generic scope.
	TypeBindings map[string]string
}

func (v BytecodeClosure) TypeName() string { return "func" }
func (v BytecodeClosure) Inspect() string {
	if v.Name != "" {
		if v.Module != "" {
			return "<closure " + v.Module + "." + v.Name + ">"
		}
		return "<closure " + v.Name + ">"
	}
	return "<closure>"
}

type BytecodeCell struct {
	Value Value
}

func (v *BytecodeCell) TypeName() string {
	if v == nil || v.Value == nil {
		return "null"
	}
	return v.Value.TypeName()
}

func (v *BytecodeCell) Inspect() string {
	if v == nil || v.Value == nil {
		return "null"
	}
	return v.Value.Inspect()
}

type BytecodeClass struct {
	Module              string
	Name                string
	Doc                 string
	Index               int64
	Parent              string
	Fields              []string
	Interfaces          []string
	Decorators          []DecoratorMetadata
	MethodDecorators    map[string][]DecoratorMetadata
	StaticDecorators    map[string][]DecoratorMetadata
	MethodMetadata      map[string][]FunctionMetadata
	StaticMetadata      map[string][]FunctionMetadata
	ConstructorMetadata []FunctionMetadata
	Immutable           bool
	// True for the class value passed to a class decorator so
	// calling the captured class from inside the decorator's
	// closure bypasses the swap and doesn't recurse.
	Raw bool
	// DefLine / DefColumn capture the source position of the
	// class declaration, surfaced by reflect.location.
	DefLine   int64
	DefColumn int64
}

func (v BytecodeClass) TypeName() string { return "class" }
func (v BytecodeClass) Inspect() string {
	if v.Module == "" {
		return "<bytecode class " + v.Name + ">"
	}
	return "<bytecode class " + v.Module + "." + v.Name + ">"
}

type NativeObject struct {
	Kind string
	ID   int64
	// Payload optionally holds a GC-managed native value (e.g. a string builder) so the handle needs no global registry entry.
	Payload any
}

func (v NativeObject) TypeName() string { return v.Kind }
func (v NativeObject) Inspect() string  { return "<" + v.Kind + ">" }

type EnumVariantDefRuntime struct {
	Name         string
	FieldCount   int
	BackingValue Value
}

type EnumDef struct {
	Name        string
	BackingType string
	Variants    []EnumVariantDefRuntime
	// Module is the enum's home module; the VM routes a method call to that
	// module's chunk when it differs from the executing VM's module.
	Module string
	// Implements names the interfaces the enum conforms to (reflect + dispatch).
	Implements []string
	// Methods holds compiled instance methods, keyed lowercased. The
	// evaluator populates Functions directly; the VM populates Indices into
	// the chunk's function table (only the latter is serialized to .gbc).
	Methods map[string][]Function
	// MethodIndices is the VM's method table: lowercased name -> chunk
	// function indices. Round-trips through the .gbc encoding.
	MethodIndices map[string][]int64
}

func (v *EnumDef) TypeName() string { return v.Name }
func (v *EnumDef) Inspect() string  { return "<enum " + v.Name + ">" }

type EnumVariant struct {
	Enum    *EnumDef
	Variant string
	Fields  []Value
}

func (v EnumVariant) TypeName() string { return v.Enum.Name }
func (v EnumVariant) Inspect() string {
	if len(v.Fields) == 0 {
		return v.Enum.Name + "." + v.Variant
	}
	parts := make([]string, 0, len(v.Fields))
	for _, f := range v.Fields {
		parts = append(parts, f.Inspect())
	}
	return v.Enum.Name + "." + v.Variant + "(" + strings.Join(parts, ", ") + ")"
}

// EnumStaticMethod implements the type-level enum surface (values/fromName)
// from one source so both backends stay byte-identical. ok is false when name
// is not a static method, letting the caller fall through to its variant logic.
// Only nullary variants participate: a bare name cannot construct a tagged one.
func EnumStaticMethod(enum *EnumDef, name string, args []Value) (Value, bool, error) {
	switch name {
	case "values":
		if len(args) != 0 {
			return nil, true, fmt.Errorf("enum %s.values expects 0 arguments, got %d", enum.Name, len(args))
		}
		elems := make([]Value, 0, len(enum.Variants))
		for _, v := range enum.Variants {
			if v.FieldCount == 0 {
				elems = append(elems, EnumVariant{Enum: enum, Variant: v.Name})
			}
		}
		return &List{Elements: elems}, true, nil
	case "fromName":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("enum %s.fromName expects 1 argument, got %d", enum.Name, len(args))
		}
		s, ok := args[0].(String)
		if !ok {
			return nil, true, fmt.Errorf("enum %s.fromName expects a string argument, got %s", enum.Name, args[0].TypeName())
		}
		for _, v := range enum.Variants {
			if v.FieldCount == 0 && v.Name == s.Value {
				return EnumVariant{Enum: enum, Variant: v.Name}, true, nil
			}
		}
		return Null{}, true, nil
	case "from", "tryFrom":
		if enum.BackingType == "" {
			return nil, true, fmt.Errorf("enum %s.%s is only available on backed enums", enum.Name, name)
		}
		if len(args) != 1 {
			return nil, true, fmt.Errorf("enum %s.%s expects 1 argument, got %d", enum.Name, name, len(args))
		}
		if args[0].TypeName() != enum.BackingType {
			return nil, true, fmt.Errorf("enum %s.%s expects a %s argument, got %s", enum.Name, name, enum.BackingType, args[0].TypeName())
		}
		for _, v := range enum.Variants {
			if valuesIdentical(args[0], v.BackingValue) {
				return EnumVariant{Enum: enum, Variant: v.Name}, true, nil
			}
		}
		if name == "tryFrom" {
			return Null{}, true, nil
		}
		return nil, true, fmt.Errorf("enum %s has no variant backed by %s", enum.Name, args[0].Inspect())
	}
	return nil, false, nil
}

func EnumVariantBackingValue(ev EnumVariant) (Value, bool) {
	if ev.Enum == nil || ev.Enum.BackingType == "" {
		return nil, false
	}
	for _, v := range ev.Enum.Variants {
		if strings.EqualFold(v.Name, ev.Variant) && v.BackingValue != nil {
			return v.BackingValue, true
		}
	}
	return nil, false
}

func valuesIdentical(left, right Value) bool {
	switch l := left.(type) {
	case String:
		r, ok := right.(String)
		return ok && l.Value == r.Value
	case SmallInt:
		switch r := right.(type) {
		case SmallInt:
			return l.Value == r.Value
		case Int:
			return r.Value.IsInt64() && l.Value == r.Value.Int64()
		}
	case Int:
		switch r := right.(type) {
		case SmallInt:
			return l.Value.IsInt64() && l.Value.Int64() == r.Value
		case Int:
			return l.Value.Cmp(r.Value) == 0
		}
	}
	return false
}

type TaskResult struct {
	Value Value
	Err   error
}

type Task struct {
	once       sync.Once
	cancelOnce sync.Once
	done       chan struct{}
	cancel     chan struct{}
	mu         sync.Mutex
	result     TaskResult
	cancelled  bool
}

func NewTask() *Task {
	return &Task{done: make(chan struct{}), cancel: make(chan struct{})}
}

func NewCompletedTask(value Value, err error) *Task {
	task := NewTask()
	task.Complete(value, err)
	return task
}

func (v *Task) TypeName() string { return "Task" }
func (v *Task) Inspect() string  { return "<Task>" }

func (v *Task) Complete(value Value, err error) {
	v.once.Do(func() {
		if value == nil {
			value = Null{}
		}
		v.mu.Lock()
		v.result = TaskResult{Value: value, Err: err}
		v.mu.Unlock()
		close(v.done)
	})
}

// Cancel signals that the task should stop. Producers that respect
// cancellation can watch CancelChan() and bail out. If the task has
// not yet completed, Cancel also resolves it with a "task cancelled"
// runtime error so any subsequent Await unblocks. Idempotent.
func (v *Task) Cancel() {
	v.cancelOnce.Do(func() {
		v.mu.Lock()
		v.cancelled = true
		v.mu.Unlock()
		close(v.cancel)
	})
	v.Complete(Null{}, ErrTaskCancelled)
}

// Cancelled reports whether Cancel has been called.
func (v *Task) Cancelled() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cancelled
}

// CancelChan exposes the cancel signal channel so async producers can
// race their own work against the cancellation.
func (v *Task) CancelChan() <-chan struct{} {
	return v.cancel
}

// DoneChan exposes the completion channel so callers can race
// completion against other channels (e.g., timeout, sibling-task
// failures).
func (v *Task) DoneChan() <-chan struct{} {
	return v.done
}

func (v *Task) Await() TaskResult {
	<-v.done
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.result
}

func (v *Task) Done() bool {
	select {
	case <-v.done:
		return true
	default:
		return false
	}
}

type Error struct {
	Class      string
	Message    string
	StackTrace string
	Fields     map[string]Value
	// Parents captures the parent class chain (immediate parent first,
	// "Error" last for the built-in chain) so cross-module `instanceof`
	// / catch on error-derived classes can walk past the chunk
	// boundary without re-looking up the source module's class table.
	Parents []string
	// Fatal marks an error that no try/catch may intercept (not even
	// catch(any)); it always unwinds to the top. Used for FatalError,
	// VM corruption, and stack-overflow conditions.
	Fatal bool
	// TraceFn lazily formats the stack trace. The VM captures a cheap
	// frame snapshot at throw time and defers the (O(frames)) string
	// formatting until errors.stackTrace / display actually needs it, so
	// a caught-and-discarded runtime fault pays no trace-formatting cost.
	// When StackTrace is non-empty it takes precedence (eager path).
	TraceFn func() string
	// TraceFrames holds the structured trace (innermost first) populated at throw time.
	TraceFrames  []StackFrame
	ErrorLine    int
	TopLevelLine int
}

func (v Error) TypeName() string { return v.Class }

// ResolvedStackTrace returns the first available trace: eager StackTrace, TraceFn, then FramesTrace.
func (v Error) ResolvedStackTrace() string {
	if v.StackTrace != "" {
		return v.StackTrace
	}
	if v.TraceFn != nil {
		return v.TraceFn()
	}
	if t := v.FramesTrace(); t != "" {
		return t
	}
	return ""
}

// FramesTrace renders TraceFrames in the stored-StackTrace convention (no leading newline, no header).
func (v Error) FramesTrace() string {
	if len(v.TraceFrames) == 0 {
		return ""
	}
	u := UncaughtError{Class: v.Class, ErrorLine: v.ErrorLine, Frames: v.TraceFrames, TopLevelLine: v.TopLevelLine}
	full := u.Render()
	idx := strings.Index(full, "\n")
	if idx < 0 {
		return ""
	}
	return strings.TrimPrefix(full[idx:], "\n")
}

// HasStackTrace reports whether any trace (eager, lazy, or structured) is available.
func (v Error) HasStackTrace() bool {
	return strings.TrimSpace(v.StackTrace) != "" || v.TraceFn != nil || len(v.TraceFrames) > 0
}

// IsFatal reports whether the error must bypass every try/catch (even
// catch(any)) and unwind to the top. True for the FatalError class and
// for errors explicitly flagged Fatal (VM corruption, stack overflow).
func (v Error) IsFatal() bool { return v.Fatal || v.Class == "FatalError" }

func (v Error) Inspect() string {
	if v.Message == "" {
		return v.Class
	}
	return v.Class + ": " + v.Message
}

type ErrorStackFrame struct {
	Function string
	Line     int64
}

func (v ErrorStackFrame) TypeName() string { return "errors.Frame" }
func (v ErrorStackFrame) Inspect() string {
	if v.Line > 0 {
		return fmt.Sprintf("<errors.Frame %s:%d>", v.Function, v.Line)
	}
	return "<errors.Frame " + v.Function + ">"
}

type ErrorStackTrace struct {
	Raw    string
	Frames []ErrorStackFrame
}

func (v ErrorStackTrace) TypeName() string { return "errors.StackTrace" }
func (v ErrorStackTrace) Inspect() string {
	return fmt.Sprintf("<errors.StackTrace %d frame(s)>", len(v.Frames))
}

type Type struct {
	Name string
}

func (v Type) TypeName() string { return "Type" }
func (v Type) Inspect() string  { return v.Name }

var builtinTypeNames = map[string]bool{
	"string": true, "int": true, "float": true, "decimal": true,
	"bool": true, "bytes": true, "any": true, "void": true,
	"null": true, "list": true, "dict": true, "set": true,
	"range": true, "func": true, "generator": true, "iterable": true,
}

func IsBuiltinTypeName(name string) bool {
	return builtinTypeNames[strings.ToLower(name)]
}

type Field struct {
	Name    string
	Type    *ast.TypeRef
	Default ast.Expression
	Doc     string // docblock (## or /** */) immediately before the field, surfaced via reflect.fields.
	// Decorators is the list of @-prefixed annotations applied to
	// the field declaration inside a class body. They are pure
	// metadata - the runtime never executes them automatically;
	// frameworks read them via `reflect.fields(cls)` to drive
	// validation, serialization, etc.
	Decorators []ast.Decorator
}

type Class struct {
	Name                 string
	Doc                  string
	Module               string
	TypeParameters       []string
	TypeParamConstraints map[string]*ast.TypeRef
	Parent               *Class
	// ParentArguments captures the type arguments supplied at class
	// declaration time. For `class Sub extends Base<string, int>` this
	// is ["string", "int"]. Empty when the parent is non-generic or
	// declared without type args.
	ParentArguments []string
	Implements      []*Interface
	Decorators      []ast.Decorator
	Fields          []Field
	Methods         map[string][]Function
	StaticMethods   map[string][]Function
	MethodMetadata  map[string][]FunctionMetadata
	StaticMetadata  map[string][]FunctionMetadata
	StaticValues    map[string]Value
	Constructors    []Function
	// Destructor is the optional `func ~ClassName()` cleanup method.
	// Nil when the class doesn't declare one. The runtime invokes it
	// at `with`-block exit (and via explicit cleanup paths added by
	// future work) - see the executor's WithStatement handling.
	Destructor *Function
	Env        *Environment
	Immutable  bool
	// ImmutableFields names the fields declared `@immutable` on this class
	// (not inherited); set-once, locked when this class's constructor completes.
	ImmutableFields []string
	// DefinitionModule / DefinitionLine / DefinitionColumn capture the
	// source position of the class declaration, surfaced by
	// reflect.location.
	DefinitionModule string
	DefinitionLine   int
	DefinitionColumn int
}

func (v *Class) TypeName() string { return "class" }
func (v *Class) Inspect() string  { return "<class " + v.Name + ">" }

// DeclaringModule prefers the evaluator's DefinitionModule, falling back to the VM's Module.
func (v *Class) DeclaringModule() string {
	if v.DefinitionModule != "" {
		return v.DefinitionModule
	}
	return v.Module
}

type Interface struct {
	Name           string
	Doc            string
	Module         string // declaring module, for module-exact instanceof
	TypeParameters []string
	Parents        []*Interface
	Methods        []*ast.FunctionSignature
	Defaults       []*ast.FunctionStatement
	Fields         []*ast.DeclarationStatement
}

func (v *Interface) TypeName() string { return "interface" }
func (v *Interface) Inspect() string  { return "<interface " + v.Name + ">" }

type Instance struct {
	Class        *Class
	Fields       map[string]Value
	TypeBindings map[string]string
	Frozen       bool
	// LockedFields holds set-once `@immutable` field names locked once their
	// declaring class's constructor completed; assigning one throws.
	LockedFields map[string]bool
	// Destroyed is set by the runtime after the class destructor
	// has run for this instance (via `del x` or the program-exit
	// sweep). The flag is one-way; once set, neither the sweep nor
	// a subsequent `del` will invoke the destructor again.
	Destroyed bool
	// Extra class names checked by `instanceof` on top of the
	// regular parent chain; set by class-decorator typed delegation.
	ExtraTypeNames []string
	// mu guards Fields while async tasks are live (see AsyncEnter).
	mu sync.Mutex
}

func (v *Instance) TypeName() string { return v.Class.Name }
func (v *Instance) Inspect() string  { return "<" + v.Class.Name + ">" }

// LockField marks a set-once `@immutable` field locked. Called when its
// declaring class's constructor completes; later writes throw.
func (v *Instance) LockField(name string) {
	if v.LockedFields == nil {
		v.LockedFields = map[string]bool{}
	}
	v.LockedFields[name] = true
}

func IsCallableValue(value Value) bool {
	switch value := value.(type) {
	case Function, OverloadedFunction, BytecodeFunction, BytecodeClosure:
		return true
	case DecoratorTarget:
		return value.Callable != nil && IsCallableValue(value.Callable)
	case *Instance:
		if value == nil || value.Class == nil {
			return false
		}
		for class := value.Class; class != nil; class = class.Parent {
			if len(class.Methods["__invoke"]) > 0 {
				return true
			}
		}
		return false
	default:
		return false
	}
}
