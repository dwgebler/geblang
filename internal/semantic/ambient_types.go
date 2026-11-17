package semantic

// ambientNativeTypeNames are bare type names the engine accepts in
// annotations without an in-file declaration (native-module class
// exports and the async Task). Kept in sync with the engine by a guard
// test in the evaluator package.
var ambientNativeTypeNames = map[string]struct{}{
	// http
	"Request": {}, "Response": {}, "Client": {}, "Builder": {},
	"CookieJar": {}, "FetchStream": {}, "Cookie": {}, "Headers": {},
	// process
	"Process": {}, "Result": {},
	// db
	"Connection": {}, "Transaction": {}, "Statement": {}, "Rows": {},
	// test
	"Test": {},
	// re / pcre / net
	"Pattern": {}, "RegexSet": {}, "IpSet": {},
	// stream interfaces
	"JsonStreamInterface": {}, "XmlStreamInterface": {},
	"YamlStreamInterface": {}, "CsvStreamInterface": {}, "LogInterface": {},
	// ambient async runtime type
	"Task": {},
}

// AmbientNativeTypeNames exposes the ambient native type-name set for the
// evaluator-package drift guard.
func AmbientNativeTypeNames() map[string]struct{} {
	out := make(map[string]struct{}, len(ambientNativeTypeNames))
	for name := range ambientNativeTypeNames {
		out[name] = struct{}{}
	}
	return out
}
