package lower

import (
	"sort"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/transpiler/emit"
	"geblang/internal/transpiler/types"
)

type Module struct {
	PackageName string
	IsEntry     bool
	IntMode     types.IntMode

	hasEntryMain        bool
	entryMainWantsArgs  bool
	entryMainReturnsInt bool

	imports          map[string]struct{}
	mainBody         *emit.Writer
	topDecls         *emit.Writer
	knownStdlib      map[string]string
	helpers          map[string]struct{}
	userModules      map[string]string
	sourceModules    map[string]struct{}
	userModuleRet    map[string]*types.Type
	classes          map[string]struct{}
	enums            map[string][]string
	interfaces       map[string]struct{}
	interfaceDecls   map[string]*ast.InterfaceStatement
	taggedVariants   map[string]int
	classMethods     map[string]map[string]struct{}
	classFields      map[string]map[string]string
	decoratedFuncs   map[string]struct{}
	funcReturns      map[string]*types.Type
	calleeParams     map[string][]string
	classParents     map[string]string
	classDecls       map[string]*ast.ClassStatement
	typeAliases      map[string]*ast.TypeRef
	calleeDefaults   map[string][]ast.Expression
	calleeVariadic   map[string]bool
	calleeParamTypes map[string][]*ast.TypeRef
	fromImports      map[string]FromImportTarget
}

// FromImportTarget resolves a from-imported local name to its origin module and
// original symbol name; IsStdlib selects native-bridge vs user-module lowering.
type FromImportTarget struct {
	Module   string
	Name     string
	IsStdlib bool
}

func NewModule(packageName string, isEntry bool, intMode types.IntMode) *Module {
	return &Module{
		PackageName: packageName,
		IsEntry:     isEntry,
		IntMode:     intMode,
		imports:     map[string]struct{}{},
		mainBody:    emit.NewWriter(),
		topDecls:    emit.NewWriter(),
		knownStdlib: map[string]string{},
		helpers:     map[string]struct{}{},
		userModules: map[string]string{},
		classes:     map[string]struct{}{},
		enums:       map[string][]string{},
		interfaces:  map[string]struct{}{},
	}
}

func (m *Module) RegisterClass(name string) {
	m.classes[name] = struct{}{}
}

func (m *Module) IsClass(name string) bool {
	_, ok := m.classes[name]
	return ok
}

// HasSubclass reports whether some user class extends name.
func (m *Module) HasSubclass(name string) bool {
	for _, parent := range m.classParents {
		if parent == name {
			return true
		}
	}
	return false
}

// ClassParent returns the immediate user-class parent of name, if any.
func (m *Module) ClassParent(name string) (string, bool) {
	p, ok := m.classParents[name]
	if !ok || p == "" || m.IsErrorClass(name) {
		return "", false
	}
	if !m.IsClass(p) {
		return "", false
	}
	return p, true
}

// InClassHierarchy reports whether name participates in a class-inheritance
// hierarchy (is extended, or extends a user class). Such classes use the
// interface-per-class dispatch scheme; standalone classes stay concrete.
func (m *Module) InClassHierarchy(name string) bool {
	if m.IsErrorClass(name) {
		return false
	}
	if m.HasSubclass(name) {
		return true
	}
	_, hasParent := m.ClassParent(name)
	return hasParent
}

// builtinErrorClasses are the engine's error hierarchy; each parents to Error unless listed in builtinErrorParents.
var builtinErrorClasses = map[string]struct{}{
	"RuntimeError": {}, "TypeError": {}, "ValueError": {}, "IOError": {},
	"ParseError": {}, "MatchError": {}, "ImmutableError": {},
	"PermissionError": {}, "AssertionError": {},
	"TimeoutError": {}, "TlsError": {},
}

// builtinErrorParents records builtin error classes that parent to another builtin (not Error directly).
var builtinErrorParents = map[string]string{
	"TimeoutError": "IOError",
	"TlsError":     "IOError",
}

func (m *Module) RegisterClassParent(name, parent string) {
	if m.classParents == nil {
		m.classParents = map[string]string{}
	}
	// Classes share one flat Go namespace; drop any module qualifier so a
	// cross-module `extends shapes.Shape` resolves to the bare class.
	if i := strings.LastIndexByte(parent, '.'); i >= 0 {
		parent = parent[i+1:]
	}
	m.classParents[name] = parent
}

func (m *Module) RegisterClassDecl(name string, decl *ast.ClassStatement) {
	if m.classDecls == nil {
		m.classDecls = map[string]*ast.ClassStatement{}
	}
	m.classDecls[name] = decl
}

func (m *Module) ClassDecl(name string) (*ast.ClassStatement, bool) {
	d, ok := m.classDecls[name]
	return d, ok
}

// ClassMethodReturnTypeRef finds the declared return type of method on the class
// whose mangled name is mangledClass, walking the parent chain. Class decls are
// keyed by Geblang name, so the receiver's mangled name is matched against each.
func (m *Module) ClassMethodReturnTypeRef(mangledClass, method string) (*ast.TypeRef, bool) {
	gb := m.gbClassNameFromMangled(mangledClass)
	for gb != "" {
		if decl, ok := m.classDecls[gb]; ok {
			for _, mem := range decl.Members {
				if fn, ok := mem.(*ast.FunctionStatement); ok && fn.Name != nil && fn.Name.Value == method {
					return fn.ReturnType, fn.ReturnType != nil
				}
			}
		}
		parent, ok := m.classParents[gb]
		if !ok {
			return nil, false
		}
		gb = parent
	}
	return nil, false
}

func (m *Module) gbClassNameFromMangled(mangledClass string) string {
	if _, ok := m.classDecls[mangledClass]; ok {
		return mangledClass
	}
	for gb := range m.classDecls {
		if emit.MangleIdent(gb) == mangledClass {
			return gb
		}
	}
	return ""
}

// IsErrorClass reports whether name's parent chain reaches the Error base.
func (m *Module) IsErrorClass(name string) bool {
	if name == "Error" {
		return true
	}
	if _, ok := builtinErrorClasses[name]; ok {
		return true
	}
	for cur := name; cur != ""; cur = m.classParents[cur] {
		if cur == "Error" {
			return true
		}
		if _, ok := builtinErrorClasses[cur]; ok {
			return true
		}
		if cur == m.classParents[cur] {
			break
		}
	}
	return false
}

// ErrorParentChain is the ancestor list (excluding name itself) up through the
// builtin Error base, used so a thrown error matches catch-by-base-class.
func (m *Module) ErrorParentChain(name string) []string {
	var chain []string
	for cur := m.classParents[name]; cur != ""; cur = m.classParents[cur] {
		chain = append(chain, cur)
		if cur == "Error" {
			return chain
		}
		if _, ok := builtinErrorClasses[cur]; ok {
			chain = append(chain, "Error")
			return chain
		}
		if cur == m.classParents[cur] {
			break
		}
	}
	if parent, ok := builtinErrorParents[name]; ok {
		chain = append(chain, parent, "Error")
		return chain
	}
	if _, ok := builtinErrorClasses[name]; ok {
		chain = append(chain, "Error")
	}
	return chain
}

func (m *Module) RegisterFunctionReturnType(name string, ret *types.Type) {
	if m.funcReturns == nil {
		m.funcReturns = map[string]*types.Type{}
	}
	m.funcReturns[name] = ret
}

func (m *Module) FunctionReturnType(name string) (*types.Type, bool) {
	if m.funcReturns == nil {
		return nil, false
	}
	t, ok := m.funcReturns[name]
	return t, ok
}

// RegisterCalleeParams records a callee's parameter order, keyed by its
// emitted Go identifier, so named arguments can be reordered.
func (m *Module) RegisterCalleeParams(key string, params []string) {
	if m.calleeParams == nil {
		m.calleeParams = map[string][]string{}
	}
	m.calleeParams[key] = params
}

func (m *Module) CalleeParams(key string) ([]string, bool) {
	if m.calleeParams == nil {
		return nil, false
	}
	p, ok := m.calleeParams[key]
	return p, ok
}

// RegisterCalleeSignature records a callee's default-arg expressions, declared
// param types, and whether the last param is variadic, keyed by the emitted Go name.
func (m *Module) RegisterCalleeSignature(key string, params []ast.Parameter) {
	if m.calleeDefaults == nil {
		m.calleeDefaults = map[string][]ast.Expression{}
		m.calleeVariadic = map[string]bool{}
		m.calleeParamTypes = map[string][]*ast.TypeRef{}
	}
	m.calleeDefaults[key] = paramDefaults(params)
	m.calleeVariadic[key] = lastVariadic(params)
	refs := make([]*ast.TypeRef, len(params))
	for i, param := range params {
		refs[i] = param.Type
	}
	m.calleeParamTypes[key] = refs
}

func (m *Module) CalleeParamTypes(key string) []*ast.TypeRef {
	if m.calleeParamTypes == nil {
		return nil
	}
	return m.calleeParamTypes[key]
}

func (m *Module) CalleeDefaults(key string) []ast.Expression {
	if m.calleeDefaults == nil {
		return nil
	}
	return m.calleeDefaults[key]
}

func (m *Module) CalleeVariadic(key string) bool {
	if m.calleeVariadic == nil {
		return false
	}
	return m.calleeVariadic[key]
}

func (m *Module) RegisterDecoratedFunction(name string) {
	if m.decoratedFuncs == nil {
		m.decoratedFuncs = map[string]struct{}{}
	}
	m.decoratedFuncs[name] = struct{}{}
}

func (m *Module) IsDecoratedFunction(name string) bool {
	if m.decoratedFuncs == nil {
		return false
	}
	_, ok := m.decoratedFuncs[name]
	return ok
}

func (m *Module) RegisterClassMethod(className, methodName string) {
	if m.classMethods == nil {
		m.classMethods = map[string]map[string]struct{}{}
	}
	if m.classMethods[className] == nil {
		m.classMethods[className] = map[string]struct{}{}
	}
	m.classMethods[className][methodName] = struct{}{}
}

func (m *Module) ClassHasMethod(className, methodName string) bool {
	if m.classMethods == nil {
		return false
	}
	if methods, ok := m.classMethods[className]; ok {
		if _, ok := methods[methodName]; ok {
			return true
		}
	}
	return false
}

func (m *Module) RegisterClassField(className, fieldName string, gbName string) {
	if m.classFields == nil {
		m.classFields = map[string]map[string]string{}
	}
	if m.classFields[className] == nil {
		m.classFields[className] = map[string]string{}
	}
	m.classFields[className][fieldName] = gbName
}

func (m *Module) ClassHasField(className, fieldName string) bool {
	if m.classFields == nil {
		return false
	}
	if fields, ok := m.classFields[className]; ok {
		if _, ok := fields[fieldName]; ok {
			return true
		}
	}
	return false
}

func (m *Module) RegisterEnum(name string, variants []string) {
	m.enums[name] = variants
}

func (m *Module) IsEnum(name string) bool {
	_, ok := m.enums[name]
	return ok
}

// IsScalarEnum reports an untagged enum (Go int-based): registered with no
// tagged variants, so its nullable form needs a pointer wrapper.
func (m *Module) IsScalarEnum(name string) bool {
	if _, ok := m.enums[name]; !ok {
		return false
	}
	for key := range m.taggedVariants {
		if strings.HasPrefix(key, name+".") {
			return false
		}
	}
	return true
}

func (m *Module) EnumHasVariant(enumName, variant string) bool {
	for _, v := range m.enums[enumName] {
		if v == variant {
			return true
		}
	}
	return false
}

func (m *Module) EnumVariants(name string) ([]string, bool) {
	v, ok := m.enums[name]
	return v, ok
}

func (m *Module) RegisterTaggedVariant(enumName, variantName string, fieldCount int) {
	if m.taggedVariants == nil {
		m.taggedVariants = map[string]int{}
	}
	m.taggedVariants[enumName+"."+variantName] = fieldCount
}

func (m *Module) IsTaggedVariant(enumName, variantName string) bool {
	if m.taggedVariants == nil {
		return false
	}
	_, ok := m.taggedVariants[enumName+"."+variantName]
	return ok
}

func (m *Module) TaggedVariantArity(enumName, variantName string) (int, bool) {
	if m.taggedVariants == nil {
		return 0, false
	}
	n, ok := m.taggedVariants[enumName+"."+variantName]
	return n, ok
}

func (m *Module) RegisterInterface(name string) {
	m.interfaces[name] = struct{}{}
}

func (m *Module) IsInterface(name string) bool {
	_, ok := m.interfaces[name]
	return ok
}

// RegisterInterfaceDecl stores the interface AST so an implementer can fold in
// any default method it does not override.
func (m *Module) RegisterInterfaceDecl(name string, s *ast.InterfaceStatement) {
	if m.interfaceDecls == nil {
		m.interfaceDecls = map[string]*ast.InterfaceStatement{}
	}
	m.interfaceDecls[name] = s
}

func (m *Module) InterfaceDecl(name string) (*ast.InterfaceStatement, bool) {
	s, ok := m.interfaceDecls[name]
	return s, ok
}

// RegisterTypeAlias records `type Name = T`; uses resolve the target inline.
func (m *Module) RegisterTypeAlias(name string, target *ast.TypeRef) {
	if m.typeAliases == nil {
		m.typeAliases = map[string]*ast.TypeRef{}
	}
	m.typeAliases[name] = target
}

func (m *Module) TypeAlias(name string) (*ast.TypeRef, bool) {
	if m.typeAliases == nil {
		return nil, false
	}
	t, ok := m.typeAliases[name]
	return t, ok
}

func (m *Module) RegisterFromImport(local string, target FromImportTarget) {
	if m.fromImports == nil {
		m.fromImports = map[string]FromImportTarget{}
	}
	m.fromImports[local] = target
}

func (m *Module) FromImport(local string) (FromImportTarget, bool) {
	if m.fromImports == nil {
		return FromImportTarget{}, false
	}
	t, ok := m.fromImports[local]
	return t, ok
}

func (m *Module) RegisterUserModule(bindingName, canonical string) {
	m.userModules[bindingName] = userModulePrefix(canonical)
}

// UserModulePrefixFor returns the Go symbol prefix for a canonical module path.
func UserModulePrefixFor(canonical string) string { return userModulePrefix(canonical) }

func (m *Module) UserModulePrefix(bindingName string) (string, bool) {
	prefix, ok := m.userModules[bindingName]
	return prefix, ok
}

func userModulePrefix(canonical string) string {
	out := emit.MangleIdent(canonical)
	out = strings.ReplaceAll(out, ".", "_")
	return out + "_"
}

func (m *Module) RequireHelper(name string) {
	m.helpers[name] = struct{}{}
}

func (m *Module) HasHelper(name string) bool {
	_, ok := m.helpers[name]
	return ok
}

func (m *Module) AddImport(path string) {
	if path == "" {
		return
	}
	m.imports[path] = struct{}{}
}

// AddTypeImports registers every import a Go type depends on, covering
// composite types whose single ImportPath field is insufficient.
func (m *Module) AddTypeImports(gt types.GoType) {
	for _, p := range gt.AllImports() {
		m.AddImport(p)
	}
}

// RegisterStdlibModule maps a stdlib import's local binding to its canonical
// name so a native-bridge lookup uses the canonical (aliased imports differ).
func (m *Module) RegisterStdlibModule(binding, canonical string) {
	m.knownStdlib[binding] = canonical
}

func (m *Module) IsStdlibModule(binding string) bool {
	_, ok := m.knownStdlib[binding]
	return ok
}

// StdlibCanonical returns the canonical module name for a stdlib binding.
func (m *Module) StdlibCanonical(binding string) string {
	if c, ok := m.knownStdlib[binding]; ok {
		return c
	}
	return binding
}

// RegisterSourceModule marks a canonical module name as provided to the
// transpiler as source AST (so its calls route to the transpiled source export,
// not the native bridge, even when the name is also a native module).
func (m *Module) RegisterSourceModule(canonical string) {
	if m.sourceModules == nil {
		m.sourceModules = map[string]struct{}{}
	}
	m.sourceModules[canonical] = struct{}{}
}

func (m *Module) IsSourceModule(canonical string) bool {
	if m.sourceModules == nil {
		return false
	}
	_, ok := m.sourceModules[canonical]
	return ok
}

// RegisterUserModuleReturn records a cross-module function's return type, keyed
// by the prefixed Go symbol, so a `module.fn()` call site can infer its result.
func (m *Module) RegisterUserModuleReturn(key string, ret *types.Type) {
	if m.userModuleRet == nil {
		m.userModuleRet = map[string]*types.Type{}
	}
	m.userModuleRet[key] = ret
}

func (m *Module) UserModuleReturn(key string) (*types.Type, bool) {
	if m.userModuleRet == nil {
		return nil, false
	}
	t, ok := m.userModuleRet[key]
	return t, ok
}

func (m *Module) MainBody() *emit.Writer { return m.mainBody }
func (m *Module) TopDecls() *emit.Writer { return m.topDecls }

func (m *Module) SetHasEntryMain(v bool) {
	m.hasEntryMain = v
	if v && (m.entryMainWantsArgs || m.entryMainReturnsInt) {
		m.AddImport("os") // os.Args / os.Exit in the entry trampoline
	}
}

// entryMainCall renders the package main() trampoline to the renamed entry main.
func (m *Module) entryMainCall() string {
	arg := ""
	if m.entryMainWantsArgs {
		arg = "os.Args[1:]"
	}
	call := EntryMainGoName + "(" + arg + ")"
	if m.entryMainReturnsInt {
		return "if __gbExit := " + call + "; __gbExit != 0 {\nos.Exit(int(__gbExit))\n}\n"
	}
	return call + "\n"
}

// SetEntryMainSignature records whether the entry main takes the args list and
// whether its int return becomes the exit code.
func (m *Module) SetEntryMainSignature(wantsArgs, returnsInt bool) {
	m.entryMainWantsArgs = wantsArgs
	m.entryMainReturnsInt = returnsInt
}

var helperRegistry = map[string]string{
	"gbFloatToInt": "func gbFloatToInt(v float64) int64 { return int64(v) }",
	"gbPtrOf":      "func gbPtrOf[T any](v T) *T { return &v }",
	"gbDecimalLit": `func gbDecimalLit(s string) *big.Rat {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		panic("invalid decimal literal: " + s)
	}
	return r
}`,
	"gbDecimalToFloat": `func gbDecimalToFloat(r *big.Rat) float64 {
	f, _ := r.Float64()
	return f
}`,
	// Inclusive integer range matching the bare range() builtin; auto picks
	// direction (+1/-1) from start vs end when no step is given.
	"gbRange": `func gbRange(start, end, step int64, auto bool) []int64 {
	if auto {
		if start > end {
			step = -1
		} else {
			step = 1
		}
	}
	if step == 0 {
		panic("range step cannot be zero")
	}
	out := []int64{}
	if step > 0 {
		for i := start; i <= end; i += step {
			out = append(out, i)
		}
	} else {
		for i := start; i >= end; i += step {
			out = append(out, i)
		}
	}
	return out
}`,
	// 1-rune string at a code-point boundary, negative index wraps; matches interpreter string indexing.
	"gbStringIndex": `func gbStringIndex(s string, i int64) string {
	rs := []rune(s)
	if i < 0 {
		i += int64(len(rs))
	}
	if i < 0 || i >= int64(len(rs)) {
		panic("string index out of range")
	}
	return string(rs[i])
}`,
	"gbUncaught": `func gbUncaught() {
	r := recover()
	if r == nil {
		return
	}
	if e, ok := r.(*transpilert.Error); ok {
		fmt.Fprintln(os.Stderr, e.Render())
		os.Exit(1)
	}
	panic(r)
}`,
	"gbTask": `type gbTask[T any] struct {
	done chan struct{}
	val  T
}

type gbAwaitable interface{ gbAwaitAny() any }

func gbRunTask[T any](fn func() T) *gbTask[T] {
	t := &gbTask[T]{done: make(chan struct{})}
	go func() {
		t.val = fn()
		close(t.done)
	}()
	return t
}

func (t *gbTask[T]) Await() T {
	<-t.done
	return t.val
}

func (t *gbTask[T]) gbAwaitAny() any { return t.Await() }`,
	"gbSleepTask": `func gbSleepTask(ms int64) *gbTask[any] {
	return gbRunTask(func() any {
		if ms > 0 {
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
		return nil
	})
}`,
	"gbAllTasks": `func gbAllTasks(tasks []any) *gbTask[[]any] {
	return gbRunTask(func() []any {
		out := make([]any, len(tasks))
		for i, t := range tasks {
			out[i] = t.(gbAwaitable).gbAwaitAny()
		}
		return out
	})
}`,
	"gbRaceTasks": `func gbRaceTasks(tasks []any) *gbTask[any] {
	return gbRunTask(func() any {
		result := make(chan any, len(tasks))
		for _, t := range tasks {
			t := t
			go func() { result <- t.(gbAwaitable).gbAwaitAny() }()
		}
		return <-result
	})
}`,
}

func helperDefinitions(required map[string]struct{}) []string {
	names := make([]string, 0, len(required))
	for name := range required {
		if _, ok := helperRegistry[name]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, helperRegistry[name])
	}
	return out
}

func (m *Module) Render() []byte {
	out := emit.NewWriter()
	out.WriteLine("package " + m.PackageName)
	out.Newline()

	if len(m.imports) > 0 {
		paths := make([]string, 0, len(m.imports))
		for p := range m.imports {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		out.WriteLine("import (")
		out.Indent()
		for _, p := range paths {
			out.WriteString(`"`)
			out.WriteString(p)
			out.WriteLine(`"`)
		}
		out.Dedent()
		out.WriteLine(")")
		out.Newline()
	}

	for _, def := range helperDefinitions(m.helpers) {
		out.WriteString(def)
		if !strings.HasSuffix(def, "\n") {
			out.WriteString("\n")
		}
		out.Newline()
	}

	if td := m.topDecls.String(); td != "" {
		out.WriteString(td)
		if !strings.HasSuffix(td, "\n") {
			out.WriteString("\n")
		}
		out.Newline()
	}

	if m.IsEntry {
		out.WriteLine("func main() {")
		out.Indent()
		if m.HasHelper("gbUncaught") {
			out.WriteLine("defer gbUncaught()")
		}
		body := m.mainBody.String()
		out.WriteString(body)
		if body != "" && !strings.HasSuffix(body, "\n") {
			out.WriteString("\n")
		}
		if m.hasEntryMain {
			out.WriteString(m.entryMainCall())
		}
		out.Dedent()
		out.WriteLine("}")
	}

	return out.Bytes()
}
