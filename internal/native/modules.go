package native

import "sort"

// NativeModuleNames is the canonical set of module names that ship as
// Geblang natives. Both `geblang check` and the LSP read this so an
// `import foo;` to a name in this set is never flagged as unresolved,
// even when the engine doesn't yet expose every function in that
// module to the LSP catalog.
var NativeModuleNames = map[string]struct{}{
	"amqp": {}, "archive_native": {}, "args": {}, "async": {}, "binary": {},
	"async.atomic": {}, "async.channel": {}, "async.sync": {}, "hnsw": {},
	"bytes": {}, "clone": {}, "cli": {}, "cli.widgets": {}, "collections": {},
	"compress": {}, "cron": {}, "crypt": {}, "csv": {}, "datetime": {}, "calendar_native": {}, "db": {},
	"dataframe": {}, "dotenv": {}, "encoding": {}, "errors": {}, "ext": {}, "ffinative": {},
	"freeze": {}, "html": {}, "http": {}, "i18n_native": {}, "imagenative": {}, "io": {}, "json": {}, "kafka": {}, "locale": {}, "log": {},
	"markdown": {}, "mailparse_native": {}, "math": {}, "metrics": {}, "msgpack": {}, "ndarray": {}, "net": {}, "onnx": {}, "browser": {}, "path": {},
	"complex": {}, "geo": {}, "stats": {}, "physics": {},
	"pcre": {}, "proc": {}, "procnative": {}, "process": {}, "profile": {},
	"profiler": {}, "random": {}, "re": {}, "reflect": {}, "resources_native": {}, "schema": {},
	"secrets": {}, "secureRandom": {}, "serde": {}, "smtp": {}, "sockets": {},
	"ssh": {}, "sshnative": {}, "store": {}, "strbuilder": {},
	"streams": {}, "string": {}, "strings": {}, "sys": {}, "template": {},
	"test": {}, "time": {}, "toml": {}, "trace": {}, "transformers": {}, "unicode": {}, "url": {},
	"uuid": {}, "vecmath": {}, "watch": {}, "web": {}, "websocket": {}, "xml": {},
	"yaml": {},
}

// IsNativeModule reports whether canonical names a Geblang-native
// module.
func IsNativeModule(canonical string) bool {
	_, ok := NativeModuleNames[canonical]
	return ok
}

// NativeModuleList returns the native module names as a sorted-by-insertion
// slice. Callers that need a deterministic order should sort themselves.
func NativeModuleList() []string {
	out := make([]string, 0, len(NativeModuleNames))
	for name := range NativeModuleNames {
		out = append(out, name)
	}
	return out
}

// ModuleDirNames returns the sorted member names dir() reports for a module,
// from the authoritative native-symbols map. Returns nil if the module is absent.
func ModuleDirNames(canonical string, symbols map[string]map[string]struct{}) []string {
	set, ok := symbols[canonical]
	if !ok {
		return nil
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
