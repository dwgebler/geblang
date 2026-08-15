// Package embedfold folds embed(...) calls into constant AST literals before either backend runs.
package embedfold

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/token"
)

type Mode int

const (
	// Inline reads each file and inlines its content.
	Inline Mode = iota
	// Validate checks shape and existence only, leaving placeholder content.
	Validate
)

// Record identifies one embedded file so callers can pin or pack it.
type Record struct {
	Path string   // cleaned, slash-separated, as written
	Abs  string   // resolved absolute disk path
	Hash [32]byte // SHA-256 of content (zero value in Validate mode)
}

// Diagnostic is one fold problem, anchored at the embed call.
type Diagnostic struct {
	Line, Column int
	Message      string
}

const embedName = "embed"

const arityMessage = "embed expects (path) or (path, {binary: true})"

// Fold rewrites every embed(...) call into an *ast.EmbeddedLiteral resolved against dir(sourcePath); idempotent, one Record per unique path.
func Fold(program *ast.Program, sourcePath string, mode Mode) ([]Record, []Diagnostic) {
	if program == nil {
		return nil, nil
	}
	f := &folder{dir: filepath.Dir(sourcePath), mode: mode, seen: map[string][]byte{}}
	rewriteProgram(program, f)
	return f.records, f.diags
}

type folder struct {
	dir     string
	mode    Mode
	seen    map[string][]byte
	records []Record
	diags   []Diagnostic
}

func (f *folder) errAt(tok token.Token, format string, args ...any) {
	f.diags = append(f.diags, Diagnostic{Line: tok.Line, Column: tok.Column, Message: fmt.Sprintf(format, args...)})
}

func (f *folder) visit(expr ast.Expression) ast.Expression {
	switch n := expr.(type) {
	case *ast.CallExpression:
		if ident, ok := embedCallee(n.Callee); ok {
			return f.foldCall(n, ident)
		}
	case *ast.Identifier:
		if n.Value == embedName {
			f.errAt(n.Token, "embed is a compile-time construct and cannot be used as a value")
		}
	}
	return expr
}

func embedCallee(callee ast.Expression) (*ast.Identifier, bool) {
	ident, ok := callee.(*ast.Identifier)
	if !ok || ident.Value != embedName {
		return nil, false
	}
	return ident, true
}

func (f *folder) foldCall(call *ast.CallExpression, ident *ast.Identifier) ast.Expression {
	if len(call.TypeArguments) > 0 {
		f.errAt(ident.Token, "embed does not take type arguments")
		return call
	}
	for _, arg := range call.Arguments {
		if arg.Name != nil {
			f.errAt(ident.Token, "embed does not accept named arguments")
			return call
		}
		if arg.Spread {
			f.errAt(ident.Token, arityMessage)
			return call
		}
	}
	if len(call.Arguments) == 0 || len(call.Arguments) > 2 {
		f.errAt(ident.Token, arityMessage)
		return call
	}
	lit, ok := call.Arguments[0].Value.(*ast.StringLiteral)
	if !ok {
		f.errAt(ident.Token, "embed path must be a string literal")
		return call
	}
	binary := false
	if len(call.Arguments) == 2 {
		if binary, ok = f.binaryOption(call.Arguments[1].Value, ident.Token); !ok {
			return call
		}
	}
	cleaned, ok := f.cleanPath(lit.Value, ident.Token)
	if !ok {
		return call
	}
	content, ok := f.load(cleaned, ident.Token)
	if !ok {
		return call
	}
	return &ast.EmbeddedLiteral{Token: ident.Token, Path: cleaned, Binary: binary, Content: content}
}

func (f *folder) binaryOption(expr ast.Expression, tok token.Token) (bool, bool) {
	dict, ok := expr.(*ast.DictLiteral)
	if !ok {
		f.errAt(tok, "embed options must be a dict literal")
		return false, false
	}
	binary := false
	for _, entry := range dict.Entries {
		key, ok := literalDictKey(entry)
		if !ok {
			f.errAt(tok, "embed options must be a dict literal")
			return false, false
		}
		if key != "binary" {
			f.errAt(tok, "unknown embed option %q", key)
			return false, false
		}
		value, ok := entry.Value.(*ast.Literal)
		if !ok {
			f.errAt(tok, "embed option binary must be true or false")
			return false, false
		}
		flag, ok := value.Value.(bool)
		if !ok {
			f.errAt(tok, "embed option binary must be true or false")
			return false, false
		}
		binary = flag
	}
	return binary, true
}

func literalDictKey(entry ast.DictEntry) (string, bool) {
	if entry.Spread || entry.Value == nil {
		return "", false
	}
	switch key := entry.Key.(type) {
	case *ast.Identifier:
		return key.Value, true
	case *ast.StringLiteral:
		return key.Value, true
	}
	return "", false
}

func (f *folder) cleanPath(raw string, tok token.Token) (string, bool) {
	if raw == "" {
		f.errAt(tok, "embed path must not be empty")
		return "", false
	}
	if strings.ContainsRune(raw, '\\') {
		f.errAt(tok, "embed path must use forward slashes")
		return "", false
	}
	// filepath.IsAbs additionally rejects a Windows volume prefix when running there.
	if path.IsAbs(raw) || filepath.IsAbs(filepath.FromSlash(raw)) {
		f.errAt(tok, "embed path must be relative to the source file")
		return "", false
	}
	cleaned := path.Clean(raw)
	for _, segment := range strings.Split(cleaned, "/") {
		if segment == ".." {
			f.errAt(tok, `embed path must not contain ".."`)
			return "", false
		}
	}
	return cleaned, true
}

func (f *folder) load(cleaned string, tok token.Token) ([]byte, bool) {
	if content, ok := f.seen[cleaned]; ok {
		return content, true
	}
	abs := filepath.Join(f.dir, filepath.FromSlash(cleaned))
	if resolved, err := filepath.Abs(abs); err == nil {
		abs = resolved
	}
	info, err := os.Stat(abs)
	if err != nil {
		f.errAt(tok, "embed cannot read %q: %v", cleaned, err)
		return nil, false
	}
	if info.IsDir() {
		f.errAt(tok, "embed path %q is a directory, not a file", cleaned)
		return nil, false
	}
	record := Record{Path: cleaned, Abs: abs}
	var content []byte
	if f.mode == Inline {
		content, err = os.ReadFile(abs)
		if err != nil {
			f.errAt(tok, "embed cannot read %q: %v", cleaned, err)
			return nil, false
		}
		record.Hash = sha256.Sum256(content)
	}
	f.seen[cleaned] = content
	f.records = append(f.records, record)
	return content, true
}
