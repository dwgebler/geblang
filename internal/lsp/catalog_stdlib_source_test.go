package lsp

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"geblang/internal/ast"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// stdlibExportAllowlist names source exports intentionally absent from
// the catalog, each with a one-line reason. Entries are "module.member"
// for a class/function or "module.Class.method" for a method.
var stdlibExportAllowlist = map[string]string{
	// datetime imports to the native module; the source DateTime class is
	// reachable only by direct file path, so it is not a catalog surface.
	"datetime.DateTime":            "native-wins on import datetime; source class reachable only by file path",
	"datetime.DateTime.toString":   "see datetime.DateTime",
	"datetime.DateTime.withYear":   "see datetime.DateTime",
	"datetime.DateTime.withMonth":  "see datetime.DateTime",
	"datetime.DateTime.withDay":    "see datetime.DateTime",
	"datetime.DateTime.withHour":   "see datetime.DateTime",
	"datetime.DateTime.withMinute": "see datetime.DateTime",
	"datetime.DateTime.withSecond": "see datetime.DateTime",

	// Internal VectorStore helpers, kept off completion as noise.
	"vectorstore.PgVectorStore.runSearch":        "internal SQL-search helper",
	"vectorstore.HnswVectorStore.filteredSearch": "internal over-fetch helper",
	"vectorstore.HnswVectorStore.toHit":          "internal record-to-hit helper",

	// Internal helpers exported only for tests, not user-facing surfaces.
	"redis.decodeReply":        "low-level RESP parser; exported for offline parser tests",
	"messaging.sqs.signingKey": "internal SigV4 signing-key helper; exported for known-vector test",
}

// sourceModuleExports parses a stdlib .gb file and returns its declared
// module name, exported function names, and exported classes mapped to
// their public non-constructor method names (original case).
func sourceModuleExports(path string) (module string, funcs []string, classes map[string][]string, err error) {
	src, readErr := os.ReadFile(path)
	if readErr != nil {
		return "", nil, nil, readErr
	}
	p := parser.New(lexer.New(string(src)))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		// A file the parser rejects cannot be audited; treat as no exports
		// rather than failing the guard on an unrelated parse issue.
		return "", nil, map[string][]string{}, nil
	}
	classes = map[string][]string{}
	for _, raw := range prog.Statements {
		if m, ok := raw.(*ast.ModuleStatement); ok {
			module = strings.Join(m.Path, ".")
			continue
		}
		es, isExport := raw.(*ast.ExportStatement)
		if !isExport {
			continue
		}
		switch s := es.Statement.(type) {
		case *ast.FunctionStatement:
			if s.Name != nil && !strings.HasPrefix(s.Name.Value, "_") {
				funcs = append(funcs, s.Name.Value)
			}
		case *ast.ClassStatement:
			if s.Name == nil || strings.HasPrefix(s.Name.Value, "_") {
				continue
			}
			cn := s.Name.Value
			methods := []string{}
			for _, mem := range s.Members {
				fn, ok := mem.(*ast.FunctionStatement)
				if !ok || fn.Name == nil {
					continue
				}
				mn := fn.Name.Value
				// A method named for its class is the constructor (the class
				// entry documents construction); private names start with _.
				if strings.HasPrefix(mn, "_") || mn == cn {
					continue
				}
				methods = append(methods, mn)
			}
			classes[cn] = methods
		}
	}
	return module, funcs, classes, nil
}

// TestCatalogCoversStdlibSourceExports makes catalog surfacing a
// permanent invariant: every exported class, public class method, and
// exported function of every stdlib .gb module (minus the allowlist)
// must be present in stdlibCatalog so the LSP offers completion/hover.
func TestCatalogCoversStdlibSourceExports(t *testing.T) {
	root := findStdlibRoot()
	if root == "" {
		t.Skip("stdlib source tree not found")
	}
	missing := []string{}
	walkErr := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".gb") {
			return nil
		}
		module, funcs, classes, perr := sourceModuleExports(p)
		if perr != nil || module == "" {
			return nil
		}
		key := module
		cat, hasCat := stdlibCatalog[key]
		report := func(member string) {
			if _, ok := stdlibExportAllowlist[module+"."+member]; ok {
				return
			}
			missing = append(missing, module+"."+member+" (catalog key "+key+")")
		}
		if !hasCat {
			for _, fn := range funcs {
				report(fn)
			}
			for cls, methods := range classes {
				report(cls)
				for _, m := range methods {
					report(cls + "." + m)
				}
			}
			return nil
		}
		for _, fn := range funcs {
			if _, ok := cat.Functions[fn]; !ok {
				report(fn)
			}
		}
		for cls, methods := range classes {
			if _, ok := cat.Classes[cls]; !ok {
				report(cls)
			}
			cm := cat.ClassMethods[cls]
			for _, m := range methods {
				if _, ok := cm[m]; !ok {
					report(cls + "." + m)
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk stdlib: %v", walkErr)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("stdlib source exports absent from the LSP catalog (%d); add them to stdlibCatalog or the allowlist with a reason:\n%s",
			len(missing), strings.Join(missing, "\n"))
	}
}
