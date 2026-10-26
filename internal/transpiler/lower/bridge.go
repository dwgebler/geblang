package lower

import (
	"strconv"

	"geblang/internal/ast"
	"geblang/internal/transpiler/emit"
	"geblang/internal/transpiler/types"
)

type BridgeEntry struct {
	GoFunc     string
	Imports    []string
	Emit       func(args []ast.Expression, ctx *EmitContext)
	ReturnType *types.Type
}

type EmitContext struct {
	Writer  *emit.Writer
	Module  *Module
	Lower   func(ast.Expression)
	AsFloat func(ast.Expression)
	// Display lowers a value for printing, rendering nullable value-types the
	// Geblang way ("null" for nil) instead of Go's pointer/<nil> default.
	Display func(ast.Expression)
}

type NativeBridge struct {
	entries map[string]BridgeEntry
}

func NewNativeBridge() *NativeBridge {
	b := &NativeBridge{entries: map[string]BridgeEntry{}}
	b.registerDefaults()
	return b
}

func (b *NativeBridge) Lookup(module, fn string) (BridgeEntry, bool) {
	e, ok := b.entries[module+"."+fn]
	return e, ok
}

func (b *NativeBridge) Register(module, fn string, entry BridgeEntry) {
	b.entries[module+"."+fn] = entry
}

func (b *NativeBridge) IsKnownModule(module string) bool {
	prefix := module + "."
	for k := range b.entries {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func (b *NativeBridge) registerDefaults() {
	b.Register("io", "print", BridgeEntry{
		Imports: []string{"fmt"},
		Emit:    func(args []ast.Expression, ctx *EmitContext) { emitPrint(args, ctx, "fmt.Print") },
	})
	b.Register("io", "println", BridgeEntry{
		Imports: []string{"fmt"},
		Emit:    func(args []ast.Expression, ctx *EmitContext) { emitPrint(args, ctx, "fmt.Println") },
	})
	b.Register("sys", "exit", BridgeEntry{
		Imports: []string{"os"},
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			ctx.Writer.WriteString("os.Exit(int(")
			if len(args) > 0 {
				ctx.Lower(args[0])
			} else {
				ctx.Writer.WriteString("0")
			}
			ctx.Writer.WriteString("))")
		},
	})
	b.Register("sys", "args", BridgeEntry{
		Imports: []string{"os"},
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			ctx.Writer.WriteString("os.Args[1:]")
		},
		ReturnType: &types.Type{Kind: types.KindList, Elem: &types.Type{Kind: types.KindString}},
	})
	b.registerMathDefaults()
	b.registerReflectDefaults()
	b.registerAsyncDefaults()
	b.registerIODefaults()
	b.registerJSONDefaults()
	b.registerDatetimeDefaults()
	b.registerSysDefaults()
	b.registerCollectionsDefaults()
	b.registerTimeDefaults()
	b.registerProfilerDefaults()
	b.registerRandomDefaults()
	b.registerEncodingDefaults()
	b.registerCryptDefaults()
	b.registerReDefaults()
	b.registerBytesDefaults()
	b.registerURLDefaults()
	b.registerCSVDefaults()
	b.registerXMLDefaults()
	b.registerTemplateDefaults()
}

// registerTemplateDefaults bridges the stdlib-backed template module
// (html/template). renderString is a free function; Template/Engine are opaque
// handles whose chained methods route via lowerTemplate*Method.
func (b *NativeBridge) registerTemplateDefaults() {
	str := &types.Type{Kind: types.KindString}
	b.Register("template", "renderString", transpilertCall("TemplateRenderString", str))
	b.Register("template", "Template", transpilertCall("NewTemplate", TemplateValueType()))
	b.Register("template", "load", transpilertCall("TemplateLoad", TemplateValueType()))
	b.Register("template", "Engine", transpilertCall("NewTemplateEngine", TemplateEngineType()))
}

// registerXMLDefaults bridges the stdlib-backed xml module (encoding/xml).
// tryParse/parseAs/validateDetailed need result-class shaping and are left to
// diagnose rather than approximate.
func (b *NativeBridge) registerXMLDefaults() {
	str := &types.Type{Kind: types.KindString}
	anyT := &types.Type{Kind: types.KindAny}
	boolT := &types.Type{Kind: types.KindBool}
	b.Register("xml", "parse", transpilertCall("XMLParse", anyT))
	b.Register("xml", "stringify", transpilertCall("XMLStringify", str))
	b.Register("xml", "validate", transpilertCall("XMLValidate", boolT))
}

// registerBytesDefaults bridges the stdlib-backed bytes module (hex/base64 over
// encoding/hex + encoding/base64). The optional utf-8 encoding arg on
// fromString/toString routes to the *Encoding helper variant.
func (b *NativeBridge) registerBytesDefaults() {
	str := &types.Type{Kind: types.KindString}
	bytesT := &types.Type{Kind: types.KindBytes}
	b.Register("bytes", "fromString", BridgeEntry{
		Imports: []string{types.OrderedDictImport},
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			emitBytesEncodingArg("BytesFromString", "BytesFromStringEncoding", args, ctx)
		},
		ReturnType: bytesT,
	})
	b.Register("bytes", "toString", BridgeEntry{
		Imports: []string{types.OrderedDictImport},
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			emitBytesEncodingArg("BytesToString", "BytesToStringEncoding", args, ctx)
		},
		ReturnType: str,
	})
	b.Register("bytes", "fromList", transpilertCall("BytesFromList", bytesT))
	b.Register("bytes", "fromHex", transpilertCall("BytesFromHex", bytesT))
	b.Register("bytes", "toHex", transpilertCall("BytesToHex", str))
	b.Register("bytes", "fromBase64", transpilertCall("BytesFromBase64", bytesT))
	b.Register("bytes", "toBase64", transpilertCall("BytesToBase64", str))
	b.Register("bytes", "fromBase64Url", transpilertCall("BytesFromBase64Url", bytesT))
	b.Register("bytes", "toBase64Url", transpilertCall("BytesToBase64Url", str))
	b.Register("bytes", "concat", BridgeEntry{
		Imports: []string{types.OrderedDictImport},
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			ctx.Writer.WriteString("transpilert.BytesConcat(")
			for i, a := range args {
				if i > 0 {
					ctx.Writer.WriteString(", ")
				}
				ctx.Lower(a)
			}
			ctx.Writer.WriteString(")")
		},
		ReturnType: bytesT,
	})
}

// registerURLDefaults bridges the stdlib-backed url-module free functions
// (net/url) plus url.URL: an opaque URLValue handle whose chained methods route
// via lowerURLValueMethod.
func (b *NativeBridge) registerURLDefaults() {
	str := &types.Type{Kind: types.KindString}
	anyDict := &types.Type{Kind: types.KindDict, Key: str, Value: &types.Type{Kind: types.KindAny}}
	b.Register("url", "URL", BridgeEntry{
		Imports:    []string{types.OrderedDictImport},
		GoFunc:     "transpilert.NewURL",
		ReturnType: URLValueType(),
	})
	b.Register("url", "stringify", transpilertCall("URLStringify", str))
	b.Register("url", "encode", transpilertCall("URLEncode", str))
	b.Register("url", "decode", transpilertCall("URLDecode", str))
	b.Register("url", "parse", transpilertCall("URLParse", anyDict))
	b.Register("url", "joinPath", BridgeEntry{
		Imports: []string{types.OrderedDictImport},
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			ctx.Writer.WriteString("transpilert.URLJoinPath(")
			for i, a := range args {
				if i > 0 {
					ctx.Writer.WriteString(", ")
				}
				ctx.Lower(a)
			}
			ctx.Writer.WriteString(")")
		},
		ReturnType: str,
	})
}

// registerCSVDefaults bridges the stdlib-backed csv module (encoding/csv).
func (b *NativeBridge) registerCSVDefaults() {
	str := &types.Type{Kind: types.KindString}
	rows := &types.Type{Kind: types.KindList, Elem: &types.Type{Kind: types.KindList, Elem: str}}
	dictRows := &types.Type{Kind: types.KindList, Elem: &types.Type{Kind: types.KindDict, Key: str, Value: str}}
	b.Register("csv", "parse", transpilertCall("CSVParse", rows))
	b.Register("csv", "parseDict", transpilertCall("CSVParseDict", dictRows))
	b.Register("csv", "stringify", transpilertCall("CSVStringify", str))
}

// emitBytesEncodingArg lowers fromString/toString, choosing the encoding-aware
// helper when a second (utf-8) argument is present.
func emitBytesEncodingArg(base, withEncoding string, args []ast.Expression, ctx *EmitContext) {
	fn := base
	if len(args) == 2 {
		fn = withEncoding
	}
	ctx.Writer.WriteString("transpilert." + fn + "(")
	for i, a := range args {
		if i > 0 {
			ctx.Writer.WriteString(", ")
		}
		ctx.Lower(a)
	}
	ctx.Writer.WriteString(")")
}

// registerReDefaults bridges the RE2-backed re module. replace is a free
// function; compile returns an opaque RePattern whose chained methods lower via
// lowerRePatternMethod. pcre is intentionally omitted (regexp2, non-stdlib) so
// it diagnoses cleanly rather than approximating with a different engine.
func (b *NativeBridge) registerReDefaults() {
	b.Register("re", "replace", transpilertCall("ReReplace", &types.Type{Kind: types.KindString}))
	b.Register("re", "compile", transpilertCall("ReCompile", RePatternType()))
}

// registerEncodingDefaults bridges the stdlib-backed encoding functions.
// sanitizeHtml is intentionally omitted (bluemonday is non-stdlib) so it
// diagnoses cleanly.
func (b *NativeBridge) registerEncodingDefaults() {
	str := &types.Type{Kind: types.KindString}
	bytesT := &types.Type{Kind: types.KindBytes}
	b.Register("encoding", "base64Encode", transpilertCall("EncodingBase64Encode", str))
	b.Register("encoding", "base64Decode", transpilertCall("EncodingBase64Decode", str))
	b.Register("encoding", "base64UrlEncode", transpilertCall("EncodingBase64UrlEncode", str))
	b.Register("encoding", "base64UrlDecode", transpilertCall("EncodingBase64UrlDecode", str))
	b.Register("encoding", "base32Encode", transpilertCall("EncodingBase32Encode", str))
	b.Register("encoding", "base32Decode", transpilertCall("EncodingBase32Decode", bytesT))
	b.Register("encoding", "base58Encode", transpilertCall("EncodingBase58Encode", str))
	b.Register("encoding", "base58Decode", transpilertCall("EncodingBase58Decode", bytesT))
	b.Register("encoding", "urlEncode", transpilertCall("EncodingUrlEncode", str))
	b.Register("encoding", "urlDecode", transpilertCall("EncodingUrlDecode", str))
	b.Register("encoding", "htmlEscape", transpilertCall("EncodingHtmlEscape", str))
	b.Register("encoding", "htmlUnescape", transpilertCall("EncodingHtmlUnescape", str))
}

// registerCryptDefaults bridges only the Go-stdlib-backed crypt functions
// (md5/sha1/sha256/sha512, crc32, hmacSha256[Bytes], base64Encode/Decode).
// sha3_256/blake2b/bcrypt*/argon2*/password*/aes*/chacha20*/jwt*/pki* need
// non-stdlib libs (golang.org/x/crypto) and are left to diagnose.
func (b *NativeBridge) registerCryptDefaults() {
	str := &types.Type{Kind: types.KindString}
	intT := &types.Type{Kind: types.KindInt}
	bytesT := &types.Type{Kind: types.KindBytes}
	b.Register("crypt", "md5", transpilertCall("CryptMd5", str))
	b.Register("crypt", "sha1", transpilertCall("CryptSha1", str))
	b.Register("crypt", "sha256", transpilertCall("CryptSha256", str))
	b.Register("crypt", "sha512", transpilertCall("CryptSha512", str))
	b.Register("crypt", "crc32", transpilertCall("CryptCrc32", intT))
	b.Register("crypt", "hmacSha256", transpilertCall("CryptHmacSha256", str))
	b.Register("crypt", "hmacSha256Bytes", transpilertCall("CryptHmacSha256Bytes", bytesT))
	b.Register("crypt", "base64Encode", transpilertCall("CryptBase64Encode", str))
	b.Register("crypt", "base64Decode", transpilertCall("CryptBase64Decode", str))
}

func (b *NativeBridge) registerTimeDefaults() {
	intT := &types.Type{Kind: types.KindInt}
	floatT := &types.Type{Kind: types.KindFloat}
	str := &types.Type{Kind: types.KindString}
	null := &types.Type{Kind: types.KindNull}
	b.Register("time", "now", transpilertCall("TimeNow", intT))
	b.Register("time", "unix", transpilertCall("TimeUnix", intT))
	b.Register("time", "unixMilli", transpilertCall("TimeUnixMilli", intT))
	b.Register("time", "unixMicro", transpilertCall("TimeUnixMicro", intT))
	b.Register("time", "unixNano", transpilertCall("TimeUnixNano", intT))
	b.Register("time", "monotonic", transpilertCall("TimeMonotonic", intT))
	b.Register("time", "monotonicNs", transpilertCall("TimeMonotonicNs", intT))
	b.Register("time", "elapsed", transpilertCall("TimeElapsed", intT))
	b.Register("time", "sleep", transpilertCall("TimeSleep", null))
	b.Register("time", "unixFloat", transpilertCall("TimeUnixFloat", floatT))
	b.Register("time", "elapsedFloat", transpilertCall("TimeElapsedFloat", floatT))
	b.Register("time", "humanize", transpilertCall("TimeHumanize", str))
}

func (b *NativeBridge) registerProfilerDefaults() {
	anyT := &types.Type{Kind: types.KindAny}
	dictT := &types.Type{Kind: types.KindDict, Key: &types.Type{Kind: types.KindString}, Value: anyT}
	b.Register("profiler", "snapshot", transpilertCall("ProfilerSnapshot", anyT))
	b.Register("profiler", "delta", transpilertCall("ProfilerDelta", dictT))
}

func (b *NativeBridge) registerRandomDefaults() {
	intT := &types.Type{Kind: types.KindInt}
	floatT := &types.Type{Kind: types.KindFloat}
	boolT := &types.Type{Kind: types.KindBool}
	null := &types.Type{Kind: types.KindNull}
	b.Register("random", "seed", transpilertCall("RandomSeed", null))
	b.Register("random", "next", transpilertCall("RandomNext", intT))
	b.Register("random", "intRange", transpilertCall("RandomIntRange", intT))
	b.Register("random", "float", transpilertCall("RandomFloat", floatT))
	b.Register("random", "bool", transpilertCall("RandomBool", boolT))
}

// registerCollectionsDefaults makes `import collections` resolve as stdlib. The
// list-receiver free functions are emitted by lowerCollectionsFreeFn (the same
// HOF path as the method form); these entries provide module recognition and a
// fallback return type for inference.
func (b *NativeBridge) registerCollectionsDefaults() {
	listAny := &types.Type{Kind: types.KindList, Elem: &types.Type{Kind: types.KindAny}}
	for _, fn := range []string{"map", "filter", "find", "findLast", "flatMap", "sorted"} {
		b.Register("collections", fn, BridgeEntry{ReturnType: listAny})
	}
	for _, fn := range []string{"any", "all"} {
		b.Register("collections", fn, BridgeEntry{ReturnType: &types.Type{Kind: types.KindBool}})
	}
	b.Register("collections", "reduce", BridgeEntry{ReturnType: &types.Type{Kind: types.KindAny}})
}

func (b *NativeBridge) registerSysDefaults() {
	str := &types.Type{Kind: types.KindString}
	anyT := &types.Type{Kind: types.KindAny}
	null := &types.Type{Kind: types.KindNull}
	b.Register("sys", "platform", transpilertCall("SysPlatform", str))
	b.Register("sys", "arch", transpilertCall("SysArch", str))
	b.Register("sys", "cpuCount", transpilertCall("SysCPUCount", &types.Type{Kind: types.KindInt}))
	b.Register("sys", "setGcPercent", transpilertCall("SysSetGCPercent", &types.Type{Kind: types.KindInt}))
	b.Register("sys", "getenv", transpilertCall("SysGetenv", anyT))
	b.Register("sys", "setenv", transpilertCall("SysSetenv", null))
	b.Register("sys", "cwd", transpilertCall("SysCwd", str))
	b.Register("sys", "hostname", transpilertCall("SysHostname", str))
	b.Register("sys", "username", transpilertCall("SysUsername", str))
	b.Register("sys", "environ", transpilertCall("SysEnviron",
		&types.Type{Kind: types.KindDict, Key: str, Value: str}))
}

func (b *NativeBridge) registerDatetimeDefaults() {
	str := &types.Type{Kind: types.KindString}
	intT := &types.Type{Kind: types.KindInt}
	null := &types.Type{Kind: types.KindNull}
	b.Register("datetime", "nowUnix", transpilertCall("DatetimeNowUnix", intT))
	b.Register("datetime", "unix", transpilertCall("DatetimeUnix", str))
	b.Register("datetime", "parseRFC3339", transpilertCall("DatetimeParseRFC3339", intT))
	b.Register("datetime", "format", transpilertCall("DatetimeFormat", str))
	b.Register("datetime", "formatRFC3339", transpilertCall("DatetimeFormatRFC3339", str))
	b.Register("datetime", "formatDate", transpilertCall("DatetimeFormatDate", str))
	b.Register("datetime", "formatTime", transpilertCall("DatetimeFormatTime", str))
	b.Register("datetime", "formatHTTP", transpilertCall("DatetimeFormatHTTP", str))
	b.Register("datetime", "toUtc", transpilertCall("DatetimeToUtc", str))
	b.Register("datetime", "addSeconds", transpilertCall("DatetimeAddSeconds", intT))
	b.Register("datetime", "addDays", transpilertCall("DatetimeAddDays", intT))
	b.Register("datetime", "addMonths", transpilertCall("DatetimeAddMonths", intT))
	b.Register("datetime", "addYears", transpilertCall("DatetimeAddYears", intT))
	b.Register("datetime", "make", transpilertCall("DatetimeMake", intT))
	b.Register("datetime", "weekdayName", transpilertCall("DatetimeWeekdayName", str))
	b.Register("datetime", "monthName", transpilertCall("DatetimeMonthName", str))
	b.Register("datetime", "sleep", transpilertCall("DatetimeSleep", null))
	// OO handle constructors: opaque value types whose chained methods route via
	// lowerDateTime*Method. now/nowInstant are non-deterministic but build/run.
	b.Register("datetime", "Instant", transpilertCall("NewDateTimeInstant", DateTimeInstantType()))
	b.Register("datetime", "nowInstant", transpilertCall("NewDateTimeInstantNow", DateTimeInstantType()))
	b.Register("datetime", "Duration", transpilertCall("NewDateTimeDuration", DateTimeDurationType()))
	b.Register("datetime", "Zone", transpilertCall("NewDateTimeZone", DateTimeZoneType()))
	b.Register("datetime", "diff", transpilertCall("DatetimeDiff",
		&types.Type{Kind: types.KindDict, Key: str, Value: intT}))
	// parse takes optional layout: one arg uses RFC3339, two use the layout.
	b.Register("datetime", "parse", BridgeEntry{
		Imports: []string{types.OrderedDictImport},
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			fn := "DatetimeParse"
			if len(args) == 2 {
				fn = "DatetimeParseLayout"
			}
			ctx.Writer.WriteString("transpilert." + fn + "(")
			for i, a := range args {
				if i > 0 {
					ctx.Writer.WriteString(", ")
				}
				ctx.Lower(a)
			}
			ctx.Writer.WriteString(")")
		},
		ReturnType: intT,
	})
}

func (b *NativeBridge) registerJSONDefaults() {
	str := &types.Type{Kind: types.KindString}
	anyT := &types.Type{Kind: types.KindAny}
	boolT := &types.Type{Kind: types.KindBool}
	b.Register("json", "stringify", transpilertCall("JSONStringify", str))
	b.Register("json", "parse", transpilertCall("JSONParse", anyT))
	b.Register("json", "validate", transpilertCall("JSONValidate", boolT))
}

// transpilertCall builds a bridge entry that lowers to transpilert.GoFunc(args)
// with positional argument lowering.
func transpilertCall(goFunc string, ret *types.Type) BridgeEntry {
	return BridgeEntry{
		Imports: []string{types.OrderedDictImport},
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			ctx.Writer.WriteString("transpilert.")
			ctx.Writer.WriteString(goFunc)
			ctx.Writer.WriteString("(")
			for i, a := range args {
				if i > 0 {
					ctx.Writer.WriteString(", ")
				}
				ctx.Lower(a)
			}
			ctx.Writer.WriteString(")")
		},
		ReturnType: ret,
	}
}

func (b *NativeBridge) registerIODefaults() {
	str := &types.Type{Kind: types.KindString}
	bytesT := &types.Type{Kind: types.KindBytes}
	boolT := &types.Type{Kind: types.KindBool}
	null := &types.Type{Kind: types.KindNull}
	b.Register("io", "readText", transpilertCall("ReadText", str))
	b.Register("io", "writeText", transpilertCall("WriteText", null))
	b.Register("io", "appendText", transpilertCall("AppendText", null))
	b.Register("io", "readBytes", transpilertCall("ReadBytes", bytesT))
	b.Register("io", "writeBytes", transpilertCall("WriteBytes", null))
	b.Register("io", "appendBytes", transpilertCall("AppendBytes", null))
	b.Register("io", "exists", transpilertCall("Exists", boolT))
	b.Register("io", "remove", transpilertCall("Remove", null))
	b.Register("io", "mkdir", transpilertCall("Mkdir", null))
}

func (b *NativeBridge) registerAsyncDefaults() {
	b.Register("async", "run", BridgeEntry{
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			ctx.Module.RequireHelper("gbTask")
			ctx.Writer.WriteString("gbRunTask(")
			if len(args) > 0 {
				ctx.Lower(args[0])
			}
			ctx.Writer.WriteString(")")
		},
		ReturnType: &types.Type{Kind: types.KindTask, Elem: &types.Type{Kind: types.KindAny}},
	})
	b.Register("async", "sleep", BridgeEntry{
		Imports: []string{"time"},
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			ctx.Module.RequireHelper("gbTask")
			ctx.Module.RequireHelper("gbSleepTask")
			ctx.Writer.WriteString("gbSleepTask(int64(")
			if len(args) > 0 {
				ctx.Lower(args[0])
			} else {
				ctx.Writer.WriteString("0")
			}
			ctx.Writer.WriteString("))")
		},
		ReturnType: &types.Type{Kind: types.KindTask, Elem: &types.Type{Kind: types.KindAny}},
	})
	b.Register("async", "all", BridgeEntry{
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			ctx.Module.RequireHelper("gbTask")
			ctx.Module.RequireHelper("gbAllTasks")
			ctx.Writer.WriteString("gbAllTasks(")
			emitAnySliceArg(args, ctx)
			ctx.Writer.WriteString(")")
		},
		ReturnType: &types.Type{
			Kind: types.KindTask,
			Elem: &types.Type{
				Kind: types.KindList,
				Elem: &types.Type{Kind: types.KindAny},
			},
		},
	})
	b.Register("async", "race", BridgeEntry{
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			ctx.Module.RequireHelper("gbTask")
			ctx.Module.RequireHelper("gbRaceTasks")
			ctx.Writer.WriteString("gbRaceTasks(")
			emitAnySliceArg(args, ctx)
			ctx.Writer.WriteString(")")
		},
		ReturnType: &types.Type{
			Kind: types.KindTask,
			Elem: &types.Type{Kind: types.KindAny},
		},
	})
}

func emitAnySliceArg(args []ast.Expression, ctx *EmitContext) {
	if len(args) != 1 {
		ctx.Writer.WriteString("[]any{}")
		return
	}
	if lit, ok := args[0].(*ast.ListLiteral); ok {
		ctx.Writer.WriteString("[]any{")
		for i, el := range lit.Elements {
			if i > 0 {
				ctx.Writer.WriteString(", ")
			}
			ctx.Lower(el)
		}
		ctx.Writer.WriteString("}")
		return
	}
	ctx.Writer.WriteString("func() []any { __src := ")
	ctx.Lower(args[0])
	ctx.Writer.WriteString("; __out := make([]any, len(__src)); for __i, __v := range __src { __out[__i] = __v }; return __out }()")
}

func (b *NativeBridge) registerReflectDefaults() {
	b.Register("reflect", "decorators", BridgeEntry{
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			if len(args) != 1 {
				ctx.Writer.WriteString("[]map[string]any{}")
				return
			}
			if id, ok := args[0].(*ast.Identifier); ok {
				if ctx.Module.IsClass(id.Value) {
					ctx.Writer.WriteString("__decorators_")
					ctx.Writer.WriteString(emit.MangleIdent(id.Value))
					return
				}
				if ctx.Module.IsDecoratedFunction(emit.MangleIdent(id.Value)) {
					ctx.Writer.WriteString("__decorators_")
					ctx.Writer.WriteString(emit.MangleIdent(id.Value))
					return
				}
			}
			ctx.Writer.WriteString("[]map[string]any{}")
		},
		ReturnType: &types.Type{
			Kind: types.KindList,
			Elem: &types.Type{
				Kind:  types.KindDict,
				Key:   &types.Type{Kind: types.KindString},
				Value: &types.Type{Kind: types.KindAny},
			},
		},
	})
	b.Register("reflect", "methods", BridgeEntry{
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			if len(args) != 1 {
				ctx.Writer.WriteString("[]string{}")
				return
			}
			if id, ok := args[0].(*ast.Identifier); ok && ctx.Module.IsClass(id.Value) {
				ctx.Writer.WriteString("__methods_")
				ctx.Writer.WriteString(emit.MangleIdent(id.Value))
				return
			}
			ctx.Writer.WriteString("[]string{}")
		},
		ReturnType: &types.Type{Kind: types.KindList, Elem: &types.Type{Kind: types.KindString}},
	})
	b.Register("reflect", "fields", BridgeEntry{
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			if len(args) != 1 {
				ctx.Writer.WriteString("[]string{}")
				return
			}
			if id, ok := args[0].(*ast.Identifier); ok && ctx.Module.IsClass(id.Value) {
				ctx.Writer.WriteString("__fields_")
				ctx.Writer.WriteString(emit.MangleIdent(id.Value))
				return
			}
			ctx.Writer.WriteString("[]string{}")
		},
		ReturnType: &types.Type{Kind: types.KindList, Elem: &types.Type{Kind: types.KindString}},
	})
	b.Register("reflect", "className", BridgeEntry{
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			if len(args) != 1 {
				ctx.Writer.WriteString(`""`)
				return
			}
			if id, ok := args[0].(*ast.Identifier); ok && ctx.Module.IsClass(id.Value) {
				ctx.Writer.WriteString(strconv.Quote(id.Value))
				return
			}
			ctx.Writer.WriteString(`""`)
		},
		ReturnType: &types.Type{Kind: types.KindString},
	})
}

func (b *NativeBridge) registerMathDefaults() {
	floatUnary := func(goFunc string) BridgeEntry {
		return BridgeEntry{
			Imports: []string{"math"},
			Emit: func(args []ast.Expression, ctx *EmitContext) {
				ctx.Writer.WriteString(goFunc)
				ctx.Writer.WriteString("(")
				if len(args) > 0 {
					ctx.AsFloat(args[0])
				}
				ctx.Writer.WriteString(")")
			},
			ReturnType: &types.Type{Kind: types.KindFloat},
		}
	}
	intFromFloat := func(goFunc string) BridgeEntry {
		return BridgeEntry{
			Imports: []string{"math"},
			Emit: func(args []ast.Expression, ctx *EmitContext) {
				ctx.Writer.WriteString("int64(")
				ctx.Writer.WriteString(goFunc)
				ctx.Writer.WriteString("(")
				if len(args) > 0 {
					ctx.AsFloat(args[0])
				}
				ctx.Writer.WriteString("))")
			},
			ReturnType: &types.Type{Kind: types.KindInt},
		}
	}
	floatBinary := func(goFunc string) BridgeEntry {
		return BridgeEntry{
			Imports: []string{"math"},
			Emit: func(args []ast.Expression, ctx *EmitContext) {
				ctx.Writer.WriteString(goFunc)
				ctx.Writer.WriteString("(")
				if len(args) > 0 {
					ctx.AsFloat(args[0])
				}
				ctx.Writer.WriteString(", ")
				if len(args) > 1 {
					ctx.AsFloat(args[1])
				}
				ctx.Writer.WriteString(")")
			},
			ReturnType: &types.Type{Kind: types.KindFloat},
		}
	}
	constant := func(goExpr string) BridgeEntry {
		return BridgeEntry{
			Imports: []string{"math"},
			Emit: func(args []ast.Expression, ctx *EmitContext) {
				ctx.Writer.WriteString(goExpr)
			},
			ReturnType: &types.Type{Kind: types.KindFloat},
		}
	}

	b.Register("math", "abs", floatUnary("math.Abs"))
	b.Register("math", "sqrt", floatUnary("math.Sqrt"))
	b.Register("math", "sin", floatUnary("math.Sin"))
	b.Register("math", "cos", floatUnary("math.Cos"))
	b.Register("math", "tan", floatUnary("math.Tan"))
	b.Register("math", "asin", floatUnary("math.Asin"))
	b.Register("math", "acos", floatUnary("math.Acos"))
	b.Register("math", "atan", floatUnary("math.Atan"))
	b.Register("math", "log", floatUnary("math.Log"))
	b.Register("math", "log10", floatUnary("math.Log10"))
	b.Register("math", "log2", floatUnary("math.Log2"))
	b.Register("math", "exp", floatUnary("math.Exp"))
	b.Register("math", "atan2", floatBinary("math.Atan2"))
	b.Register("math", "pow", floatBinary("math.Pow"))
	b.Register("math", "floor", intFromFloat("math.Floor"))
	b.Register("math", "ceil", intFromFloat("math.Ceil"))
	b.Register("math", "round", intFromFloat("math.Round"))
	b.Register("math", "trunc", intFromFloat("math.Trunc"))
	b.Register("math", "pi", constant("math.Pi"))
	b.Register("math", "e", constant("math.E"))
	b.Register("math", "min", BridgeEntry{
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			ctx.Writer.WriteString("min(")
			for i, a := range args {
				if i > 0 {
					ctx.Writer.WriteString(", ")
				}
				ctx.Lower(a)
			}
			ctx.Writer.WriteString(")")
		},
	})
	b.Register("math", "max", BridgeEntry{
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			ctx.Writer.WriteString("max(")
			for i, a := range args {
				if i > 0 {
					ctx.Writer.WriteString(", ")
				}
				ctx.Lower(a)
			}
			ctx.Writer.WriteString(")")
		},
	})
	b.Register("math", "clamp", BridgeEntry{
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			if len(args) != 3 {
				ctx.Writer.WriteString("0")
				return
			}
			ctx.Writer.WriteString("min(max(")
			ctx.Lower(args[0])
			ctx.Writer.WriteString(", ")
			ctx.Lower(args[1])
			ctx.Writer.WriteString("), ")
			ctx.Lower(args[2])
			ctx.Writer.WriteString(")")
		},
	})
	b.Register("math", "sign", BridgeEntry{
		Emit: func(args []ast.Expression, ctx *EmitContext) {
			ctx.Writer.WriteString("func(__v float64) int64 { switch { case __v > 0: return 1; case __v < 0: return -1; default: return 0 } }(")
			if len(args) > 0 {
				ctx.AsFloat(args[0])
			}
			ctx.Writer.WriteString(")")
		},
		ReturnType: &types.Type{Kind: types.KindInt},
	})
}

func emitPrint(args []ast.Expression, ctx *EmitContext, goFn string) {
	ctx.Writer.WriteString(goFn)
	ctx.Writer.WriteString("(")
	for i, a := range args {
		if i > 0 {
			ctx.Writer.WriteString(", ")
		}
		ctx.Display(a)
	}
	ctx.Writer.WriteString(")")
}
