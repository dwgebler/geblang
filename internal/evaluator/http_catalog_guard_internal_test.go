package evaluator

import (
	"sort"
	"strings"
	"testing"

	"geblang/internal/native"
	"geblang/internal/runtime"
)

// The http object classes are built natively, so the LSP catalog drifts unless it is checked against them.
func TestHTTPClassCatalogMatchesRuntimeMethods(t *testing.T) {
	e := runLeakProg(t, "import http;\nlet c = http.newClient({});\nlet b = http.request(\"http://x/\");\nlet j = http.newCookieJar();\n")
	classes := map[string]*runtime.Class{"Client": e.httpClientClass, "Builder": e.httpBuilderClass, "CookieJar": e.httpCookieJarClass, "FetchStream": e.httpFetchStreamClass}
	for name, cls := range classes {
		if cls == nil {
			t.Fatalf("http.%s class was not built", name)
		}
		catalogued := map[string]bool{}
		var phantom []string
		for m := range native.LookupClassMethods("http." + name) {
			catalogued[strings.ToLower(m)] = true
			if _, ok := cls.Methods[strings.ToLower(m)]; !ok {
				phantom = append(phantom, m)
			}
		}
		var missing []string
		for m := range cls.Methods {
			if !catalogued[m] {
				missing = append(missing, m)
			}
		}
		sort.Strings(phantom)
		sort.Strings(missing)
		if len(phantom) > 0 || len(missing) > 0 {
			t.Errorf("http.%s catalog drift: catalogued but not implemented %v, implemented but not catalogued %v", name, phantom, missing)
		}
	}
}
