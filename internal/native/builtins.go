package native

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"strings"
	"sync"

	"geblang/internal/runtime"
)

// builtinFunctionsOnce ensures the read-only functions map of the
// builtin registry is constructed exactly once per process. Per-VM
// NewBuiltinRegistry calls then share the same map and only pay for
// the small per-VM patches overlay - shaving ~500 string allocs
// per VM creation that the registerX cascade would otherwise repeat.
var (
	builtinFunctionsOnce sync.Once
	builtinFunctions     map[string]Function
)

func ensureBuiltinFunctions() map[string]Function {
	builtinFunctionsOnce.Do(func() {
		r := NewRegistry()
		registerAllBuiltins(r)
		builtinFunctions = r.functions
	})
	return builtinFunctions
}

// NewBuiltinRegistry returns a Registry pre-populated with all pure built-in functions.
// The function map is built once per process and shared across VMs;
// each call still allocates a fresh per-VM patches overlay so tests
// can install mocks without disturbing other VMs.
func NewBuiltinRegistry() *Registry {
	return &Registry{
		functions: ensureBuiltinFunctions(),
		patches:   map[string]Function{},
	}
}

// registerAllBuiltins is the original NewBuiltinRegistry body, lifted
// out so the one-time initialisation in ensureBuiltinFunctions can
// reuse it. Direct callers should not invoke this - go through
// NewBuiltinRegistry.
func registerAllBuiltins(r *Registry) {
	registerMath(r)
	registerMathAggregates(r)
	registerVecmath(r)
	registerTokenizer(r)
	registerPooling(r)
	registerHnsw(r)
	registerJSON(r)
	registerCSV(r)
	registerXML(r)
	registerHTML(r)
	registerTOML(r)
	registerYAML(r)
	registerCrypt(r)
	registerCryptPKI(r)
	registerCryptX509(r)
	registerCryptJWK(r)
	registerDatetime(r)
	registerCalendarNative(r)
	registerResourcesNative(r)
	registerSecrets(r)
	registerRandom(r)
	registerSecureRandom(r)
	registerMsgpack(r)
	registerUnicode(r)
	registerCron(r)
	registerAsyncSync(r)
	registerAsyncAtomic(r)
	registerAsyncChannel(r)
	registerStore(r)
	registerImage(r)
	registerTime(r)
	registerBytes(r)
	registerString(r)
	registerStringBuilder(r)
	registerCompress(r)
	registerArchive(r)
	registerArchiveStream(r)
	registerLocale(r)
	registerMailparse(r)
	registerEncoding(r)
	registerBinary(r)
	registerURL(r)
	registerUUID(r)
	registerRe(r)
	registerRegexCompile(r)
	registerNDArray(r)
	registerStats(r)
	registerStatsDescriptive(r)
	registerComplex(r)
	registerGeo(r)
	registerPhysics(r)
	registerDataFrame(r)
	registerDataFrameIO(r)
	registerPCRE(r)
	registerMarkdown(r)
	registerTemplate(r)
	registerSys(r)
	registerProcess(r)
	registerArgs(r)
	registerErrors(r)
	registerFreeze(r)
	registerClone(r)
	registerProfiler(r)
}

// PureModuleNames returns the function names registered in a pure module, plus a found flag.
func PureModuleNames(module string) ([]string, bool) {
	fns, ok := pureBuiltins[module]
	if !ok {
		return nil, false
	}
	names := make([]string, 0, len(fns))
	for name := range fns {
		names = append(names, name)
	}
	return names, true
}

func dictBool(dict runtime.Dict, key string) bool {
	value, ok := dictLookup(dict, key)
	if !ok {
		return false
	}
	boolValue, ok := value.(runtime.Bool)
	return ok && boolValue.Value
}

func singleBool(args []runtime.Value, label string) (bool, error) {
	if len(args) != 1 {
		return false, fmt.Errorf("%s expects exactly one argument", label)
	}
	value, ok := args[0].(runtime.Bool)
	if !ok {
		return false, fmt.Errorf("%s argument must be bool", label)
	}
	return value.Value, nil
}

func dictLookup(dict runtime.Dict, key string) (runtime.Value, bool) {
	keyValue := runtime.String{Value: key}
	entry, ok := dict.GetEntry(DictKey(keyValue))
	if !ok {
		return nil, false
	}
	return entry.Value, true
}

func dictString(dict runtime.Dict, key string) string {
	value, ok := dictLookup(dict, key)
	if !ok {
		return ""
	}
	text, ok := value.(runtime.String)
	if !ok {
		return ""
	}
	return text.Value
}

func dictInt64(dict runtime.Dict, key string) (int64, bool) {
	value, ok := dictLookup(dict, key)
	if !ok {
		return 0, false
	}
	n, ok := AsInt64(value)
	return n, ok
}

func singleString(args []runtime.Value, label string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("%s expects exactly one argument", label)
	}
	value, ok := args[0].(runtime.String)
	if !ok {
		return "", fmt.Errorf("%s expects a string argument", label)
	}
	return value.Value, nil
}

// parseAsArgs validates the two-argument shape shared by
// json.parseAs/yaml.parseAs/toml.parseAs/xml.parseAs:
// (text: string, target: class).
func parseAsArgs(args []runtime.Value, label string) (string, runtime.Value, error) {
	if len(args) != 2 {
		return "", nil, fmt.Errorf("%s expects (text, class)", label)
	}
	text, ok := args[0].(runtime.String)
	if !ok {
		return "", nil, fmt.Errorf("%s expects a string for the first argument", label)
	}
	if !isClassValue(args[1]) {
		return "", nil, fmt.Errorf("%s expects a class for the second argument", label)
	}
	return text.Value, args[1], nil
}

// isClassValue reports whether the value is a class reference
// (either evaluator's *runtime.Class, the VM's BytecodeClass, or a
// DecoratorTarget with Target=="class" - the compile-time precomputed
// form the VM hands back from `reflect.class("Name")` for a literal
// name argument).
func isClassValue(value runtime.Value) bool {
	switch v := value.(type) {
	case *runtime.Class:
		return true
	case runtime.BytecodeClass:
		return true
	case runtime.DecoratorTarget:
		return v.Target == "class"
	}
	return false
}

func singleBytes(args []runtime.Value, label string) ([]byte, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", label)
	}
	value, ok := args[0].(runtime.Bytes)
	if !ok {
		return nil, fmt.Errorf("%s expects a bytes argument", label)
	}
	return value.Value, nil
}

func singleInt64(args []runtime.Value, label string) (int64, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("%s expects exactly one argument", label)
	}
	n, ok := AsInt64(args[0])
	if !ok {
		return 0, fmt.Errorf("%s expects an integer argument", label)
	}
	return n, nil
}

func stringWithOptionalUTF8Encoding(args []runtime.Value, label string) (string, error) {
	if len(args) != 1 && len(args) != 2 {
		return "", fmt.Errorf("%s expects text and optional encoding", label)
	}
	text, ok := args[0].(runtime.String)
	if !ok {
		return "", fmt.Errorf("%s text must be string", label)
	}
	if len(args) == 2 {
		encoding, ok := args[1].(runtime.String)
		if !ok || !strings.EqualFold(encoding.Value, "utf-8") {
			return "", fmt.Errorf("%s only supports utf-8 encoding", label)
		}
	}
	return text.Value, nil
}

func bytesWithOptionalUTF8Encoding(args []runtime.Value, label string) ([]byte, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, fmt.Errorf("%s expects bytes and optional encoding", label)
	}
	data, ok := args[0].(runtime.Bytes)
	if !ok {
		return nil, fmt.Errorf("%s data must be bytes", label)
	}
	if len(args) == 2 {
		encoding, ok := args[1].(runtime.String)
		if !ok || !strings.EqualFold(encoding.Value, "utf-8") {
			return nil, fmt.Errorf("%s only supports utf-8 encoding", label)
		}
	}
	return data.Value, nil
}

func secureRandomBytes(args []runtime.Value, label string) ([]byte, error) {
	size, err := singleInt64(args, label)
	if err != nil {
		return nil, err
	}
	if size < 0 || size > 1<<20 {
		return nil, fmt.Errorf("%s byte count out of range", label)
	}
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return nil, err
	}
	return data, nil
}

func secretComparableBytes(value runtime.Value) ([]byte, error) {
	switch value := value.(type) {
	case runtime.String:
		return []byte(value.Value), nil
	case runtime.Bytes:
		return value.Value, nil
	default:
		return nil, fmt.Errorf("secret comparison expects string or bytes")
	}
}

func constantTimeEqual(left []byte, right []byte) bool {
	key := []byte("geblang.secrets.constantTimeEqual.v1")
	leftMAC := hmac.New(sha256.New, key)
	_, _ = leftMAC.Write(left)
	rightMAC := hmac.New(sha256.New, key)
	_, _ = rightMAC.Write(right)
	digestEqual := subtle.ConstantTimeCompare(leftMAC.Sum(nil), rightMAC.Sum(nil))
	lengthEqual := constantTimeIntEqual(len(left), len(right))
	return digestEqual&lengthEqual == 1
}

func constantTimeIntEqual(left int, right int) int {
	diff := uint64(left ^ right)
	diff |= diff >> 32
	diff |= diff >> 16
	diff |= diff >> 8
	diff |= diff >> 4
	diff |= diff >> 2
	diff |= diff >> 1
	return int((diff & 1) ^ 1)
}
