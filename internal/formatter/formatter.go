// Package formatter provides canonical source formatting for Geblang programs; it preserves comments and refuses (never corrupts) any output that would not re-parse to the same AST.
package formatter

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// Format parses src, formats it canonically, and returns the result.
// Returns a parse error if src is not valid Geblang.
func Format(src []byte) ([]byte, error) {
	return FormatWithOptions(src, Options{})
}

// Options selects a formatting mode (zero value preserves style; Clean strips redundant parens + flattens chains; StripComments drops comments).
type Options struct {
	Clean         bool
	StripComments bool
}

// FormatWithOptions formats src under opts; the round-trip self-check applies to every mode.
func FormatWithOptions(src []byte, opts Options) (result []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("formatter bug: panic while formatting (%v); source left unchanged", r)
		}
	}()
	p := parser.New(lexer.New(string(src)))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		return nil, fmt.Errorf("parse error: %s", errs[0])
	}
	f := &fmtr{clean: opts.Clean}
	if !opts.StripComments {
		f.comments = p.Comments()
	}
	f.program(program)
	out := strings.TrimRight(f.buf.String(), "\n") + "\n"
	if err := verifyRoundTrip(program, out); err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// verifyRoundTrip refuses any output that does not re-parse to the same AST, so fmt can never silently change a program's meaning or break its syntax.
func verifyRoundTrip(input *ast.Program, out string) error {
	p := parser.New(lexer.New(out))
	reparsed := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		return fmt.Errorf("formatter bug: output no longer parses (%s); source left unchanged", errs[0])
	}
	if input.String() != reparsed.String() {
		return fmt.Errorf("formatter bug: formatting would change the program's meaning; source left unchanged")
	}
	return nil
}

type fmtr struct {
	buf             bytes.Buffer
	depth           int
	prevWasTopLevel bool
	comments        []lexer.Comment
	ci              int
	clean           bool
}

// flushTrailing appends a comment that sits on the same source line as the statement just written, keeping it on that line instead of moving it.
func (f *fmtr) flushTrailing(line int) {
	for f.ci < len(f.comments) && f.comments[f.ci].Line == line {
		b := f.buf.Bytes()
		if len(b) > 0 && b[len(b)-1] == '\n' {
			f.buf.Truncate(f.buf.Len() - 1)
		}
		f.buf.WriteString(" " + renderComment(f.comments[f.ci]) + "\n")
		f.ci++
	}
}

// startLine returns a statement's first source line via its Token field, or a large sentinel for nodes without one.
func startLine(s ast.Statement) int {
	v := reflect.Indirect(reflect.ValueOf(s))
	if v.Kind() == reflect.Struct {
		if tok := v.FieldByName("Token"); tok.IsValid() && tok.Kind() == reflect.Struct {
			if line := tok.FieldByName("Line"); line.IsValid() && line.CanInt() {
				return int(line.Int())
			}
		}
	}
	return 1 << 30
}

// endLine returns the largest source line in a statement's subtree (its last token), so blank-line gaps after multi-line statements are measured correctly.
func endLine(s ast.Statement) int {
	max := 0
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		if !v.IsValid() {
			return
		}
		switch v.Kind() {
		case reflect.Ptr, reflect.Interface:
			walk(v.Elem())
		case reflect.Struct:
			if v.Type().Name() == "Token" {
				if line := v.FieldByName("Line"); line.IsValid() && line.CanInt() {
					if n := int(line.Int()); n > max {
						max = n
					}
				}
				return
			}
			for i := 0; i < v.NumField(); i++ {
				if fv := v.Field(i); fv.CanInterface() {
					walk(fv)
				}
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Map:
			for _, k := range v.MapKeys() {
				walk(v.MapIndex(k))
			}
		}
	}
	walk(reflect.ValueOf(s))
	return max
}

// isTopDecl reports whether a top-level statement is a declaration that is always separated by a blank line.
func isTopDecl(s ast.Statement) bool {
	switch x := s.(type) {
	case *ast.FunctionStatement, *ast.ClassStatement, *ast.InterfaceStatement, *ast.EnumStatement:
		return true
	case *ast.ExportStatement:
		return isTopDecl(x.Statement)
	}
	return false
}

// maybeBlank emits one blank line when the source had a gap (>1 line) between the previous statement's end and the next statement or its leading comment.
func (f *fmtr) maybeBlank(prevEnd int, s ast.Statement) {
	if prevEnd <= 0 {
		return
	}
	next := startLine(s)
	if f.ci < len(f.comments) && f.comments[f.ci].Line < next {
		next = f.comments[f.ci].Line
	}
	if next-prevEnd > 1 {
		f.nl()
	}
}

// flushComments emits every pending comment whose source line precedes beforeLine.
func (f *fmtr) flushComments(beforeLine int) {
	for f.ci < len(f.comments) && f.comments[f.ci].Line < beforeLine {
		f.writeln(f.pad() + renderComment(f.comments[f.ci]))
		f.ci++
	}
}

func renderComment(c lexer.Comment) string {
	switch c.Kind {
	case "doc-line":
		if c.Text == "" {
			return "##"
		}
		return "## " + c.Text
	case "block":
		if strings.Contains(c.Text, "\n") {
			return "/*" + c.Text + "*/"
		}
		return "/* " + strings.TrimSpace(c.Text) + " */"
	case "doc-block":
		if strings.Contains(c.Text, "\n") {
			return "/**" + c.Text + "*/"
		}
		return "/** " + strings.TrimSpace(c.Text) + " */"
	default:
		if c.Text == "" {
			return "#"
		}
		return "# " + c.Text
	}
}

func (f *fmtr) pad() string { return strings.Repeat("    ", f.depth) }

func (f *fmtr) write(s string)   { f.buf.WriteString(s) }
func (f *fmtr) writeln(s string) { f.buf.WriteString(s); f.buf.WriteByte('\n') }
func (f *fmtr) nl()              { f.buf.WriteByte('\n') }

// ---- top level ----

func (f *fmtr) program(prog *ast.Program) {
	// Module/import statements first (no blank line between them), then a blank
	// line before other declarations.
	stmts := prog.Statements
	i := 0
	// imports and module declarations
	for i < len(stmts) {
		switch stmts[i].(type) {
		case *ast.ModuleStatement, *ast.ImportStatement, *ast.FromImportStatement:
			f.flushComments(startLine(stmts[i]))
			f.stmt(stmts[i])
			f.flushTrailing(endLine(stmts[i]))
			i++
			continue
		}
		break
	}
	// Top-level declarations are always blank-separated; other statements keep the author's blanks.
	prevEnd := 0
	var prev ast.Statement
	for i < len(stmts) {
		if i > 0 {
			if isTopDecl(stmts[i]) || isTopDecl(prev) {
				f.nl()
			} else {
				f.maybeBlank(prevEnd, stmts[i])
			}
		}
		f.flushComments(startLine(stmts[i]))
		f.stmt(stmts[i])
		f.flushTrailing(endLine(stmts[i]))
		prevEnd = endLine(stmts[i])
		prev = stmts[i]
		i++
	}
	f.flushComments(1 << 30)
}

func importPath(path []string, forceBuiltin bool) string {
	if forceBuiltin {
		return ast.ReservedModuleNamespace + "." + strings.Join(path, ".")
	}
	return strings.Join(path, ".")
}

// ---- statements ----

func (f *fmtr) stmt(s ast.Statement) {
	switch s := s.(type) {
	case *ast.ModuleStatement:
		f.writeln("module " + strings.Join(s.Path, ".") + ";")
	case *ast.ImportStatement:
		line := f.pad() + "import " + importPath(s.Path, s.ForceBuiltin)
		if s.Alias != nil {
			line += " as " + s.Alias.Value
		}
		f.writeln(line + ";")
	case *ast.FromImportStatement:
		names := make([]string, 0, len(s.Names))
		for _, n := range s.Names {
			name := n.Name.Value
			if n.Alias != nil {
				name += " as " + n.Alias.Value
			}
			names = append(names, name)
		}
		f.writeln(f.pad() + "from " + importPath(s.Path, s.ForceBuiltin) + " import " + strings.Join(names, ", ") + ";")
	case *ast.ExportStatement:
		f.write(f.pad() + "export ")
		f.stmtInner(s.Statement)
	case *ast.TypeAliasStatement:
		f.writeln(f.pad() + "type " + s.Name.Value + " = " + s.Type.String() + ";")
	case *ast.DeclarationStatement:
		f.fmtDeclaration(s)
	case *ast.DestructuringStatement:
		f.fmtDestructuring(s)
	case *ast.ExpressionStatement:
		f.writeln(f.pad() + f.renderValue(s.Expression, len(f.pad())) + ";")
	case *ast.ReturnStatement:
		if s.Value == nil {
			f.writeln(f.pad() + "return;")
		} else if lit, ok := s.Value.(*ast.ListLiteral); ok && lit.Bare {
			f.writeln(f.pad() + "return " + f.bareElements(lit) + ";")
		} else {
			prefix := f.pad() + "return "
			f.writeln(prefix + f.renderValue(s.Value, len(prefix)) + ";")
		}
	case *ast.YieldStatement:
		if s.Value == nil {
			f.writeln(f.pad() + "yield;")
		} else {
			f.writeln(f.pad() + "yield " + f.expr(s.Value) + ";")
		}
	case *ast.SimpleStatement:
		if s.Value == nil {
			f.writeln(f.pad() + s.Kind + ";")
		} else {
			f.writeln(f.pad() + s.Kind + " " + f.expr(s.Value) + ";")
		}
	case *ast.IfStatement:
		f.fmtIf(s)
	case *ast.WhileStatement:
		f.write(f.pad() + "while (" + f.expr(s.Condition) + ") ")
		f.block(s.Body)
	case *ast.ForStatement:
		f.fmtFor(s)
	case *ast.FunctionStatement:
		f.fmtFunction(s)
	case *ast.ClassStatement:
		f.fmtClass(s)
	case *ast.InterfaceStatement:
		f.fmtInterface(s)
	case *ast.EnumStatement:
		f.fmtEnum(s)
	case *ast.TryStatement:
		f.fmtTry(s)
	case *ast.MatchStatement:
		f.fmtMatch(s)
	case *ast.InitStatement:
		f.write(f.pad() + "init ")
		f.block(s.Body)
	case *ast.WithStatement:
		head := "with ("
		if s.Name != nil {
			head += s.Name.Value + " = "
		}
		head += f.expr(s.Value) + ") "
		f.write(f.pad() + head)
		f.block(s.Body)
	case *ast.SelectStatement:
		f.writeln(f.pad() + "select {")
		f.depth++
		for _, c := range s.Cases {
			var sc string
			if c.Kind == "send" {
				sc = f.expr(c.Channel) + ".send(" + f.expr(c.Value) + ")"
			} else {
				if c.Binding != "" {
					sc = "let " + c.Binding + " = "
				}
				sc += f.expr(c.Channel) + ".recv()"
			}
			f.write(f.pad() + "case " + sc + ": ")
			f.block(c.Body)
		}
		if s.Default != nil {
			f.write(f.pad() + "default: ")
			f.block(s.Default)
		}
		f.depth--
		f.writeln(f.pad() + "}")
	default:
		// Fallback: use AST's own String() method
		f.writeln(f.pad() + s.String() + ";")
	}
}

// stmtInner writes a statement without the leading pad (used after "export ").
func (f *fmtr) stmtInner(s ast.Statement) {
	switch s := s.(type) {
	case *ast.FunctionStatement:
		f.fmtFunctionRaw(s)
	case *ast.ClassStatement:
		f.fmtClassRaw(s)
	case *ast.InterfaceStatement:
		f.fmtInterface(s)
	case *ast.EnumStatement:
		f.fmtEnum(s)
	default:
		f.stmtNoPrefix(s)
	}
}

func (f *fmtr) stmtNoPrefix(s ast.Statement) {
	switch s := s.(type) {
	case *ast.DeclarationStatement:
		line := strings.Join(declPrefix(s), " ")
		if s.Value != nil {
			line += " = " + f.expr(s.Value)
		}
		f.writeln(line + ";")
	default:
		f.writeln(s.String() + ";")
	}
}

// ---- declaration ----

// declPrefix renders the qualifier + optional type + name of a declaration, preserving const/static (Kind) which a typed-only render would drop.
func declPrefix(s *ast.DeclarationStatement) []string {
	var parts []string
	if s.Kind != "" {
		parts = append(parts, s.Kind)
	}
	if s.Type != nil {
		parts = append(parts, s.Type.String())
	}
	if len(parts) == 0 {
		parts = append(parts, "let")
	}
	parts = append(parts, s.Name.Value)
	return parts
}

func (f *fmtr) fmtDecorator(dec ast.Decorator) string {
	if len(dec.Arguments) > 0 {
		return "@" + dec.Name.Value + "(" + f.callArgs(dec.Arguments) + ")"
	}
	return "@" + dec.Name.Value
}

func (f *fmtr) typeArgs(args []*ast.TypeRef) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = a.String()
	}
	return "<" + strings.Join(parts, ", ") + ">"
}

func (f *fmtr) fmtDeclaration(s *ast.DeclarationStatement) {
	line := f.pad() + strings.Join(declPrefix(s), " ")
	if s.Value != nil {
		prefix := line + " = "
		line = prefix + f.renderValue(s.Value, len(prefix))
	}
	f.writeln(line + ";")
}

func (f *fmtr) fmtDestructuring(s *ast.DestructuringStatement) {
	var lhs string
	if s.IsList && s.Bare {
		parts := make([]string, 0, len(s.Names))
		for _, n := range s.Names {
			parts = append(parts, n.Value)
		}
		lhs = strings.Join(parts, ", ")
	} else if s.IsList {
		parts := make([]string, 0, len(s.Names))
		for _, n := range s.Names {
			parts = append(parts, n.Value)
		}
		lhs = "[" + strings.Join(parts, ", ") + "]"
	} else {
		parts := make([]string, 0, len(s.Names))
		for i, n := range s.Names {
			entry := n.Value
			if i < len(s.Keys) && s.Keys[i] != n.Value {
				entry = s.Keys[i] + ": " + n.Value
			}
			parts = append(parts, entry)
		}
		lhs = "{" + strings.Join(parts, ", ") + "}"
	}
	kw := "let "
	if !s.Define {
		kw = ""
	}
	rhs := f.expr(s.Value)
	if lit, ok := s.Value.(*ast.ListLiteral); ok && lit.Bare {
		rhs = f.bareElements(lit)
	}
	f.writeln(f.pad() + kw + lhs + " = " + rhs + ";")
}

// bareElements renders list elements comma-separated, without brackets.
func (f *fmtr) bareElements(lit *ast.ListLiteral) string {
	parts := make([]string, 0, len(lit.Elements))
	for _, el := range lit.Elements {
		parts = append(parts, f.expr(el))
	}
	return strings.Join(parts, ", ")
}

// ---- if ----

func (f *fmtr) fmtIf(s *ast.IfStatement) {
	f.write(f.pad() + "if (" + f.expr(s.Condition) + ") ")
	f.blockInline(s.Consequence, func() {
		for _, ei := range s.ElseIfs {
			f.write(" else if (" + f.expr(ei.Condition) + ") ")
			f.blockInline(ei.Body, nil)
		}
		if s.Alternative != nil {
			f.write(" else ")
			f.block(s.Alternative)
		} else if len(s.ElseIfs) == 0 {
			f.nl() // a trailing else-if block already emits its own line terminator
		}
	})
}

// blockInline writes a block and calls after() before the trailing newline
// (used to chain else/else-if on the same line as the closing brace).
func (f *fmtr) blockInline(block *ast.BlockStatement, after func()) {
	if block == nil || len(block.Statements) == 0 {
		if after != nil {
			f.write("{}")
			after()
		} else {
			f.writeln("{}")
		}
		return
	}
	f.writeln("{")
	f.depth++
	prevEnd := 0
	for _, s := range block.Statements {
		f.maybeBlank(prevEnd, s)
		f.flushComments(startLine(s))
		f.stmt(s)
		f.flushTrailing(endLine(s))
		prevEnd = endLine(s)
	}
	f.depth--
	f.write(f.pad() + "}")
	if after != nil {
		after()
	} else {
		f.nl()
	}
}

// block writes a block with its own trailing newline.
func (f *fmtr) block(block *ast.BlockStatement) {
	f.blockInline(block, nil)
}

// ---- for ----

func (f *fmtr) fmtFor(s *ast.ForStatement) {
	if s.VarName != nil || len(s.VarNames) > 0 {
		// for-in loop
		rangeStr := f.expr(s.Iterable)
		if s.Step != nil {
			rangeStr += " by " + f.expr(s.Step)
		}
		var vars string
		if len(s.VarNames) > 0 {
			parts := make([]string, 0, len(s.VarNames))
			for _, v := range s.VarNames {
				parts = append(parts, v.Value)
			}
			vars = strings.Join(parts, ", ")
		} else {
			if s.VarType != nil {
				vars = s.VarType.String() + " "
			}
			vars += s.VarName.Value
		}
		f.write(f.pad() + "for (" + vars + " in " + rangeStr + ") ")
		f.block(s.Body)
		return
	}
	// C-style for
	var initStr, condStr, updateStr string
	if s.Init != nil {
		// strip trailing newline/semicolon from statement rendering
		tmp := &fmtr{depth: 0, clean: f.clean}
		tmp.stmt(s.Init)
		initStr = strings.TrimRight(tmp.buf.String(), "\n;")
	}
	if s.Condition != nil {
		condStr = f.expr(s.Condition)
	}
	if s.Update != nil {
		tmp := &fmtr{depth: 0, clean: f.clean}
		tmp.stmt(s.Update)
		updateStr = strings.TrimRight(tmp.buf.String(), "\n;")
	}
	f.write(f.pad() + "for (" + initStr + "; " + condStr + "; " + updateStr + ") ")
	f.block(s.Body)
}

// ---- function ----

func (f *fmtr) fmtFunction(s *ast.FunctionStatement) {
	f.write(f.pad())
	f.fmtFunctionRaw(s)
}

func (f *fmtr) fmtFunctionRaw(s *ast.FunctionStatement) {
	for i, dec := range s.Decorators {
		if i > 0 {
			f.write(f.pad())
		}
		f.writeln(f.fmtDecorator(dec))
	}
	if len(s.Decorators) > 0 {
		f.write(f.pad())
	}
	prefix := ""
	if s.Static {
		prefix += "static "
	}
	if s.Async {
		prefix += "async "
	}
	prefix += "func " + s.Name.Value
	if len(s.Generics) > 0 {
		prefix += "<" + f.typeParams(s.Generics) + ">"
	}
	prefix += "(" + f.params(s.Parameters) + ")"
	if s.ReturnType != nil {
		prefix += ": " + s.ReturnType.String()
	}
	prefix += " "
	f.write(prefix)
	f.block(s.Body)
}

// ---- class ----

func (f *fmtr) fmtClass(s *ast.ClassStatement) {
	f.write(f.pad())
	f.fmtClassRaw(s)
}

func (f *fmtr) fmtClassRaw(s *ast.ClassStatement) {
	for i, dec := range s.Decorators {
		if i > 0 {
			f.write(f.pad())
		}
		f.writeln(f.fmtDecorator(dec))
	}
	if len(s.Decorators) > 0 {
		f.write(f.pad())
	}
	line := "class " + s.Name.Value
	if len(s.Generics) > 0 {
		line += "<" + f.typeParams(s.Generics) + ">"
	}
	if s.Extends != nil {
		line += " extends " + s.Extends.String()
	}
	if len(s.Implements) > 0 {
		parts := make([]string, 0, len(s.Implements))
		for _, iface := range s.Implements {
			parts = append(parts, iface.String())
		}
		line += " implements " + strings.Join(parts, ", ")
	}
	f.writeln(line + " {")
	f.depth++
	prevEnd := 0
	for _, m := range s.Members {
		f.maybeBlank(prevEnd, m)
		f.flushComments(startLine(m))
		f.stmt(m)
		f.flushTrailing(endLine(m))
		prevEnd = endLine(m)
	}
	f.depth--
	f.writeln(f.pad() + "}")
}

// ---- interface ----

func (f *fmtr) fmtInterface(s *ast.InterfaceStatement) {
	line := f.pad() + "interface " + s.Name.Value
	if len(s.Generics) > 0 {
		line += "<" + f.typeParams(s.Generics) + ">"
	}
	if len(s.Parents) > 0 {
		parts := make([]string, 0, len(s.Parents))
		for _, p := range s.Parents {
			parts = append(parts, p.String())
		}
		line += " extends " + strings.Join(parts, ", ")
	}
	f.writeln(line + " {")
	f.depth++
	for _, m := range s.Methods {
		f.fmtSignature(m)
	}
	f.depth--
	f.writeln(f.pad() + "}")
}

func (f *fmtr) fmtSignature(sig *ast.FunctionSignature) {
	line := f.pad() + "func " + sig.Name.Value
	if len(sig.Generics) > 0 {
		line += "<" + f.typeParams(sig.Generics) + ">"
	}
	line += "(" + f.params(sig.Parameters) + ")"
	if sig.ReturnType != nil {
		line += ": " + sig.ReturnType.String()
	}
	f.writeln(line + ";")
}

// ---- enum ----

func (f *fmtr) fmtEnum(s *ast.EnumStatement) {
	header := "enum " + s.Name.Value
	if len(s.Implements) > 0 {
		parts := make([]string, 0, len(s.Implements))
		for _, iface := range s.Implements {
			parts = append(parts, iface.String())
		}
		header += " implements " + strings.Join(parts, ", ")
	}
	f.writeln(f.pad() + header + " {")
	f.depth++
	for i, v := range s.Variants {
		line := f.pad() + v.Name.Value
		if len(v.FieldTypes) > 0 {
			parts := make([]string, 0, len(v.FieldTypes))
			for _, ft := range v.FieldTypes {
				parts = append(parts, ft.String())
			}
			line += "(" + strings.Join(parts, ", ") + ")"
		}
		if i < len(s.Variants)-1 {
			line += ","
		} else if len(s.Methods) > 0 {
			line += ";"
		}
		f.writeln(line)
	}
	for _, m := range s.Methods {
		f.fmtFunction(m)
	}
	f.depth--
	f.writeln(f.pad() + "}")
}

// ---- try ----

func (f *fmtr) fmtTry(s *ast.TryStatement) {
	f.write(f.pad() + "try ")
	f.blockInline(s.Body, func() {
		for _, catch := range s.Catches {
			clause := " catch"
			if catch.Type != nil || catch.Name != nil {
				clause += " ("
				if catch.Type != nil {
					clause += catch.Type.String()
				}
				if catch.Name != nil {
					if catch.Type != nil {
						clause += " "
					}
					clause += catch.Name.Value
				}
				clause += ")"
			}
			f.write(clause + " ")
			f.blockInline(catch.Body, nil)
		}
		if s.Finally != nil && len(s.Finally.Statements) > 0 {
			f.write(" finally ")
			f.block(s.Finally)
		} else if len(s.Catches) == 0 {
			f.nl() // only the no-catch/no-finally form needs an explicit line terminator; a catch/finally block already emits one
		}
	})
}

// ---- match ----

func (f *fmtr) fmtMatch(s *ast.MatchStatement) {
	f.writeln(f.pad() + "match (" + f.expr(s.Expr) + ") {")
	f.depth++
	for _, c := range s.Cases {
		f.fmtMatchCase(c)
	}
	f.depth--
	f.writeln(f.pad() + "}")
}

func (f *fmtr) fmtMatchCase(c ast.MatchCase) {
	head := "default"
	if !c.Default {
		head = "case " + f.matchCasePattern(c)
	}
	if c.Body == nil { // arrow form: `case pat => value;`
		f.writeln(f.pad() + head + " => " + f.expr(c.Value) + ";")
		return
	}
	f.write(f.pad() + head + ": ")
	f.block(c.Body)
}

func (f *fmtr) matchCasePattern(c ast.MatchCase) string {
	var pat string
	switch {
	case c.EnumVariant != nil:
		pat = f.enumVariantPattern(c.EnumVariant)
	case c.ListPattern != nil:
		pat = f.listPattern(c.ListPattern)
	case c.Type != nil:
		pat = c.Type.String()
		if c.Name != nil {
			pat += " " + c.Name.Value
		}
	default:
		pat = f.matchPattern(c.Pattern)
	}
	for _, alt := range c.Alternates {
		pat += " | " + f.expr(alt)
	}
	if c.Guard != nil {
		pat += " if (" + f.expr(c.Guard) + ")"
	}
	return pat
}

func (f *fmtr) enumVariantPattern(p *ast.EnumVariantPattern) string {
	s := p.Enum.Value + "." + p.Variant.Value
	if len(p.Params) > 0 {
		parts := make([]string, len(p.Params))
		for i, pp := range p.Params {
			seg := ""
			if pp.Type != nil {
				seg = pp.Type.String() + " "
			}
			if pp.Name != nil {
				seg += pp.Name.Value
			}
			parts[i] = seg
		}
		s += "(" + strings.Join(parts, ", ") + ")"
	}
	return s
}

func (f *fmtr) listPattern(p *ast.ListPatternMatch) string {
	parts := make([]string, len(p.Bindings))
	for i, b := range p.Bindings {
		if b.Literal != nil {
			parts[i] = f.expr(b.Literal)
			continue
		}
		seg := ""
		if b.Type != nil {
			seg = b.Type.String() + " "
		}
		if b.Name != nil {
			seg += b.Name.Value
		}
		parts[i] = seg
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func (f *fmtr) matchPattern(p ast.Expression) string {
	if p == nil {
		return "_"
	}
	return f.expr(p)
}

// ---- expressions ----

// Precedence ladder mirroring internal/parser so expr() can parenthesize exactly where re-association would otherwise change meaning.
const (
	fpLowest = iota
	fpAssign
	fpPipe
	fpTernary
	fpNullCoalesce
	fpLogicalOr
	fpLogicalAnd
	fpBitOr
	fpBitXor
	fpBitAnd
	fpEquality
	fpCompare
	fpShift
	fpSum
	fpProduct
	fpPower
	fpPrefix
	fpPostfix
	fpCall
)

var fmtInfixPrec = map[string]int{
	"??": fpNullCoalesce, "|>": fpPipe,
	"||": fpLogicalOr, "xor": fpLogicalOr, "&&": fpLogicalAnd,
	"|": fpBitOr, "^": fpBitXor, "&": fpBitAnd,
	"==": fpEquality, "!=": fpEquality, "is": fpEquality, "is not": fpEquality, "instanceof": fpEquality,
	"in": fpCompare, "<": fpCompare, "<=": fpCompare, ">": fpCompare, ">=": fpCompare,
	"<<": fpShift, ">>": fpShift,
	"+": fpSum, "-": fpSum,
	"*": fpProduct, "/": fpProduct, "//": fpProduct, "%": fpProduct,
	"**": fpPower,
}

func fmtPrec(e ast.Expression) int {
	switch x := e.(type) {
	case *ast.InfixExpression:
		if p, ok := fmtInfixPrec[x.Operator]; ok {
			return p
		}
		return fpLowest + 1 // unknown operator: parenthesize defensively
	case *ast.PrefixExpression, *ast.AwaitExpression:
		return fpPrefix
	case *ast.PostfixExpression:
		return fpPostfix
	case *ast.CastExpression, *ast.RangeExpression:
		return fpCompare
	case *ast.TernaryExpression:
		return fpTernary
	case *ast.AssignmentExpression:
		return fpAssign
	case *ast.SpreadExpression:
		return fpLowest
	default:
		return fpCall
	}
}

// fmtIsPrimary reports whether e can sit unparenthesized to the left of a postfix tail (call/index/selector).
func fmtIsPrimary(e ast.Expression) bool {
	switch e.(type) {
	case *ast.InfixExpression, *ast.PrefixExpression, *ast.PostfixExpression,
		*ast.CastExpression, *ast.TernaryExpression, *ast.AssignmentExpression,
		*ast.RangeExpression, *ast.AwaitExpression, *ast.SpreadExpression:
		return false
	}
	return true
}

func parenIf(cond bool, s string) string {
	if cond {
		return "(" + s + ")"
	}
	return s
}

// nodeLine returns an expression's source line via its Token field, or 0.
func nodeLine(e ast.Expression) int {
	v := reflect.Indirect(reflect.ValueOf(e))
	if v.Kind() == reflect.Struct {
		if tok := v.FieldByName("Token"); tok.IsValid() && tok.Kind() == reflect.Struct {
			if line := tok.FieldByName("Line"); line.IsValid() && line.CanInt() {
				return int(line.Int())
			}
		}
	}
	return 0
}

// flattenInfix collects the operands of a left-leaning chain of one operator.
func flattenInfix(e *ast.InfixExpression, op string) []ast.Expression {
	var operands []ast.Expression
	var walk func(n ast.Expression)
	walk = func(n ast.Expression) {
		if inf, ok := n.(*ast.InfixExpression); ok && inf.Operator == op {
			walk(inf.Left)
			operands = append(operands, inf.Right)
			return
		}
		operands = append(operands, n)
	}
	walk(e)
	return operands
}

// shouldBreakInfix reports whether a +/&&/|| chain of 3+ operands spanned multiple source lines (an intentional author line break to preserve).
func (f *fmtr) shouldBreakInfix(e *ast.InfixExpression) bool {
	if f.clean {
		return false
	}
	op := e.Operator
	if op != "+" && op != "&&" && op != "||" {
		return false
	}
	operands := flattenInfix(e, op)
	if len(operands) < 3 {
		return false
	}
	minL, maxL := 1<<30, 0
	for _, o := range operands {
		l := nodeLine(o)
		if l == 0 {
			continue
		}
		if l < minL {
			minL = l
		}
		if l > maxL {
			maxL = l
		}
	}
	return maxL > minL
}

type chainSeg struct {
	name     string
	optional bool
	call     *ast.CallExpression // nil for a bare property access
	line     int
}

// flattenChain peels a `.method(args)` / `.prop` selector-call spine off e, returning the base object and the segments in source order.
func flattenChain(e ast.Expression) (ast.Expression, []chainSeg) {
	var segs []chainSeg
	node := e
	for {
		if call, ok := node.(*ast.CallExpression); ok {
			if sel, ok := call.Callee.(*ast.SelectorExpression); ok && !sel.Parenthesized {
				segs = append(segs, chainSeg{name: sel.Name.Value, optional: sel.Optional, call: call, line: nodeLine(sel.Name)})
				node = sel.Object
				continue
			}
		}
		if sel, ok := node.(*ast.SelectorExpression); ok && !sel.Parenthesized {
			segs = append(segs, chainSeg{name: sel.Name.Value, optional: sel.Optional, line: nodeLine(sel.Name)})
			node = sel.Object
			continue
		}
		break
	}
	for i, j := 0, len(segs)-1; i < j; i, j = i+1, j-1 {
		segs[i], segs[j] = segs[j], segs[i]
	}
	return node, segs
}

// shouldBreakChain reports whether a method chain has 2+ segments the author split across source lines.
func (f *fmtr) shouldBreakChain(e ast.Expression) bool {
	if f.clean {
		return false
	}
	base, segs := flattenChain(e)
	if len(segs) < 2 {
		return false
	}
	minL, maxL := nodeLine(base), 0
	if minL == 0 {
		minL = 1 << 30
	}
	for _, s := range segs {
		if s.line == 0 {
			continue
		}
		if s.line < minL {
			minL = s.line
		}
		if s.line > maxL {
			maxL = s.line
		}
	}
	return maxL > minL
}

// multilineChain renders a method chain keeping the author's line breaks: each segment that started on a new source line goes on its own indented line.
func (f *fmtr) multilineChain(e ast.Expression) string {
	base, segs := flattenChain(e)
	indent := strings.Repeat("    ", f.depth+1)
	var b strings.Builder
	b.WriteString(f.exprChild(base, !fmtIsPrimary(base)))
	prevLine := nodeLine(base)
	for _, s := range segs {
		dot := "."
		if s.optional {
			dot = "?."
		}
		seg := dot + s.name
		if s.call != nil {
			seg += f.typeArgs(s.call.TypeArguments) + "(" + f.callArgs(s.call.Arguments) + ")"
		}
		if prevLine > 0 && s.line > prevLine {
			b.WriteString("\n" + indent + seg)
		} else {
			b.WriteString(seg)
		}
		prevLine = s.line
	}
	return b.String()
}

// multilineInfix renders a chain with each continuation operand on its own line, operator-first, indented one level past the statement.
func (f *fmtr) multilineInfix(e *ast.InfixExpression) string {
	op := e.Operator
	operands := flattenInfix(e, op)
	self := fmtPrec(e)
	indent := strings.Repeat("    ", f.depth+1)
	var b strings.Builder
	for i, o := range operands {
		wrap := fmtPrec(o) <= self
		if i == 0 {
			wrap = fmtPrec(o) < self
		}
		s := f.exprChild(o, wrap)
		if i == 0 {
			b.WriteString(s)
		} else {
			b.WriteString("\n" + indent + op + " " + s)
		}
	}
	return b.String()
}

// sliceIndex renders a range used as an index in Python slice syntax: start:end[:step].
func (f *fmtr) sliceIndex(r *ast.RangeExpression) string {
	s := ""
	if r.Start != nil {
		s += f.expr(r.Start)
	}
	s += ":"
	if r.End != nil {
		s += f.expr(r.End)
	}
	if r.Step != nil {
		s += ":" + f.expr(r.Step)
	}
	return s
}

// isParen reports whether an explicit grouping paren should be preserved; clean mode drops them.
func (f *fmtr) isParen(e ast.Expression) bool {
	if f.clean {
		return false
	}
	switch x := e.(type) {
	case *ast.InfixExpression:
		return x.Parenthesized
	case *ast.CastExpression:
		return x.Parenthesized
	case *ast.TernaryExpression:
		return x.Parenthesized
	case *ast.PrefixExpression:
		return x.Parenthesized
	case *ast.SelectorExpression:
		return x.Parenthesized
	}
	return false
}

// expr renders an expression, keeping the author's explicit parentheses.
func (f *fmtr) expr(e ast.Expression) string {
	return parenIf(f.isParen(e), f.exprBare(e))
}

// exprChild renders a child expression, wrapping it once if precedence requires it OR the author parenthesized it.
func (f *fmtr) exprChild(e ast.Expression, needs bool) string {
	return parenIf(needs || f.isParen(e), f.exprBare(e))
}

func (f *fmtr) exprBare(e ast.Expression) string {
	if e == nil {
		return ""
	}
	switch e := e.(type) {
	case *ast.Identifier:
		return e.Value
	case *ast.Literal:
		switch v := e.Value.(type) {
		case nil:
			return "null"
		case bool:
			if v {
				return "true"
			}
			return "false"
		default:
			return fmt.Sprintf("%v", v)
		}
	case *ast.IntegerLiteral:
		return e.Token.Literal
	case *ast.DecimalLiteral:
		return e.Token.Literal
	case *ast.FloatLiteral:
		return e.Token.Literal
	case *ast.StringLiteral:
		return f.fmtString(e.Raw, e.Quote, e.Triple)
	case *ast.InterpolatedString:
		return f.fmtInterpolated(e)
	case *ast.ListLiteral:
		parts := make([]string, 0, len(e.Elements))
		for _, el := range e.Elements {
			parts = append(parts, f.expr(el))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *ast.DictLiteral:
		parts := make([]string, 0, len(e.Entries))
		for _, entry := range e.Entries {
			if entry.Spread {
				parts = append(parts, "..."+f.expr(entry.Value))
				continue
			}
			parts = append(parts, f.expr(entry.Key)+": "+f.expr(entry.Value))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *ast.SetLiteral:
		parts := make([]string, 0, len(e.Elements))
		for _, el := range e.Elements {
			parts = append(parts, f.expr(el))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *ast.PipeExpression:
		return f.expr(e.Left) + " |> " + f.expr(e.Right)
	case *ast.ListComprehension:
		return "[" + f.expr(e.Body) + f.fmtComprehensionClauses(e.Clauses) + "]"
	case *ast.SetComprehension:
		return "{" + f.expr(e.Body) + f.fmtComprehensionClauses(e.Clauses) + "}"
	case *ast.DictComprehension:
		return "{" + f.expr(e.KeyBody) + ": " + f.expr(e.ValueBody) + f.fmtComprehensionClauses(e.Clauses) + "}"
	case *ast.PrefixExpression:
		sep := ""
		if n := len(e.Operator); n > 0 && e.Operator[n-1] >= 'a' && e.Operator[n-1] <= 'z' {
			sep = " "
		}
		return e.Operator + sep + f.exprChild(e.Right, fmtPrec(e.Right) < fpPrefix)
	case *ast.PostfixExpression:
		return f.exprChild(e.Left, fmtPrec(e.Left) < fpPostfix) + e.Operator
	case *ast.InfixExpression:
		if f.shouldBreakInfix(e) {
			return f.multilineInfix(e)
		}
		self := fmtPrec(e)
		leftWrap := fmtPrec(e.Left) < self
		rightWrap := fmtPrec(e.Right) <= self
		if e.Operator == "**" { // right-associative
			leftWrap = fmtPrec(e.Left) <= self
			rightWrap = fmtPrec(e.Right) < self
		}
		if _, isCast := e.Left.(*ast.CastExpression); isCast && (e.Operator == "|" || e.Operator == "&") {
			leftWrap = true // a cast's type greedily absorbs a following | / & as a union/intersection type
		}
		return f.exprChild(e.Left, leftWrap) + " " + e.Operator + " " + f.exprChild(e.Right, rightWrap)
	case *ast.AssignmentExpression:
		// The parser desugars `a op= b` into `a = a op b` sharing the one left node.
		if infix, ok := e.Value.(*ast.InfixExpression); ok && infix.Left == e.Left {
			return f.expr(e.Left) + " " + infix.Operator + "= " + f.expr(infix.Right)
		}
		return f.expr(e.Left) + " = " + f.expr(e.Value)
	case *ast.SelectorExpression:
		if f.shouldBreakChain(e) {
			return f.multilineChain(e)
		}
		dot := "."
		if e.Optional {
			dot = "?."
		}
		return f.exprChild(e.Object, !fmtIsPrimary(e.Object)) + dot + e.Name.Value
	case *ast.CallExpression:
		if f.shouldBreakChain(e) {
			return f.multilineChain(e)
		}
		return f.exprChild(e.Callee, !fmtIsPrimary(e.Callee)) + f.typeArgs(e.TypeArguments) + "(" + f.callArgs(e.Arguments) + ")"
	case *ast.PartialExpression:
		if f.shouldBreakChain(e) {
			return f.multilineChain(e)
		}
		return f.exprChild(e.Callee, !fmtIsPrimary(e.Callee)) + f.typeArgs(e.TypeArguments) + "(" + f.partialArgs(e.Arguments) + ")"
	case *ast.IndexExpression:
		idx := f.expr(e.Index)
		if rng, ok := e.Index.(*ast.RangeExpression); ok {
			idx = f.sliceIndex(rng) // a range as an index is Python-style slice syntax
		}
		return f.exprChild(e.Left, !fmtIsPrimary(e.Left)) + "[" + idx + "]"
	case *ast.SpreadExpression:
		return "..." + f.expr(e.Value)
	case *ast.RangeExpression:
		op := ".."
		if e.Exclusive {
			op = "..<"
		}
		s := f.expr(e.Start) + op + f.expr(e.End)
		if e.Step != nil {
			s += " by " + f.expr(e.Step)
		}
		return s
	case *ast.FunctionLiteral:
		prefix := "func"
		if e.Async {
			prefix = "async func"
		}
		sig := prefix + "(" + f.params(e.Parameters) + ")"
		if e.ReturnType != nil {
			sig += ": " + e.ReturnType.String()
		}
		// Inline small bodies; block format larger ones
		body := f.inlineBlock(e.Body)
		return sig + " " + body
	case *ast.AwaitExpression:
		return "await " + f.expr(e.Value)
	case *ast.CastExpression:
		return f.exprChild(e.Value, fmtPrec(e.Value) < fpCompare) + " as " + e.Type.String()
	case *ast.TernaryExpression:
		cond := f.exprChild(e.Condition, fmtPrec(e.Condition) <= fpTernary)
		then := f.exprChild(e.ThenExpr, fmtPrec(e.ThenExpr) < fpTernary)
		els := f.exprChild(e.ElseExpr, fmtPrec(e.ElseExpr) < fpTernary)
		return cond + " ? " + then + " : " + els
	case *ast.MatchExpression:
		return f.fmtMatchExpr(e)
	default:
		return e.String()
	}
}

func (f *fmtr) fmtString(raw string, quote byte, triple bool) string {
	q := string(quote)
	if triple {
		return q + q + q + raw + q + q + q
	}
	return q + raw + q
}

func (f *fmtr) fmtInterpolated(e *ast.InterpolatedString) string {
	quote := e.Token.Quote
	if quote == 0 {
		quote = '"'
	}
	q := string(quote)
	var sb strings.Builder
	if e.Token.Triple {
		sb.WriteString(q + q + q)
	} else {
		sb.WriteString(q)
	}
	for _, p := range e.Parts {
		switch part := p.(type) {
		case *ast.StringLiteral:
			sb.WriteString(part.Raw)
		case *ast.FormattedInterpolation:
			sb.WriteString("${")
			sb.WriteString(f.expr(part.Value))
			sb.WriteString(":")
			sb.WriteString(part.Spec)
			sb.WriteString("}")
		default:
			sb.WriteString("${")
			sb.WriteString(f.expr(p))
			sb.WriteString("}")
		}
	}
	if e.Token.Triple {
		sb.WriteString(q + q + q)
	} else {
		sb.WriteString(q)
	}
	return sb.String()
}

func (f *fmtr) inlineBlock(block *ast.BlockStatement) string {
	if block == nil || len(block.Statements) == 0 {
		return "{}"
	}
	if len(block.Statements) == 1 {
		tmp := &fmtr{depth: 0, clean: f.clean}
		tmp.stmt(block.Statements[0])
		inner := strings.TrimRight(tmp.buf.String(), "\n")
		return "{ " + inner + " }"
	}
	// Multi-statement: write as indented block (returns string representation)
	var b strings.Builder
	b.WriteString("{\n")
	inner := &fmtr{depth: f.depth + 1, comments: f.comments, ci: f.ci, clean: f.clean}
	prevEnd := 0
	for _, s := range block.Statements {
		inner.maybeBlank(prevEnd, s)
		inner.flushComments(startLine(s))
		inner.stmt(s)
		inner.flushTrailing(endLine(s))
		prevEnd = endLine(s)
	}
	f.ci = inner.ci
	b.WriteString(inner.buf.String())
	b.WriteString(strings.Repeat("    ", f.depth) + "}")
	return b.String()
}

func (f *fmtr) fmtMatchExpr(e *ast.MatchExpression) string {
	var b strings.Builder
	b.WriteString("match (" + f.expr(e.Expr) + ") {\n")
	inner := &fmtr{depth: f.depth + 1, clean: f.clean}
	for _, c := range e.Cases {
		inner.fmtMatchCase(c)
	}
	b.WriteString(inner.buf.String())
	b.WriteString(strings.Repeat("    ", f.depth) + "}")
	return b.String()
}

// ---- helpers ----

const fmtMaxWidth = 100

func isCollection(e ast.Expression) bool {
	switch e.(type) {
	case *ast.ListLiteral, *ast.DictLiteral, *ast.SetLiteral:
		return true
	}
	return false
}

// spanMultiline reports whether the source lines span more than one line.
func spanMultiline(lines []int) bool {
	lo, hi := 1<<30, 0
	for _, l := range lines {
		if l == 0 {
			continue
		}
		if l < lo {
			lo = l
		}
		if l > hi {
			hi = l
		}
	}
	return hi > lo
}

// elementLines returns the source line of each element, entry, or argument of a collection or call.
func elementLines(e ast.Expression) []int {
	var lines []int
	switch e := e.(type) {
	case *ast.ListLiteral:
		for _, el := range e.Elements {
			lines = append(lines, nodeLine(el))
		}
	case *ast.SetLiteral:
		for _, el := range e.Elements {
			lines = append(lines, nodeLine(el))
		}
	case *ast.DictLiteral:
		for _, en := range e.Entries {
			if en.Spread || en.Key == nil {
				lines = append(lines, nodeLine(en.Value))
			} else {
				lines = append(lines, nodeLine(en.Key))
			}
		}
	case *ast.CallExpression:
		for _, a := range e.Arguments {
			lines = append(lines, nodeLine(a.Value))
		}
	case *ast.PartialExpression:
		for _, a := range e.Arguments {
			if !a.Hole {
				lines = append(lines, nodeLine(a.Value))
			}
		}
	}
	return lines
}

// needsBreakAuthor reports whether a collection or call (or any nested one) was written across multiple source lines, so an enclosing construct must break to let the inner one expand.
func needsBreakAuthor(e ast.Expression) bool {
	switch e := e.(type) {
	case *ast.ListLiteral:
		if spanMultiline(elementLines(e)) {
			return true
		}
		for _, el := range e.Elements {
			if needsBreakAuthor(el) {
				return true
			}
		}
	case *ast.SetLiteral:
		if spanMultiline(elementLines(e)) {
			return true
		}
		for _, el := range e.Elements {
			if needsBreakAuthor(el) {
				return true
			}
		}
	case *ast.DictLiteral:
		if spanMultiline(elementLines(e)) {
			return true
		}
		for _, en := range e.Entries {
			if needsBreakAuthor(en.Value) {
				return true
			}
		}
	case *ast.CallExpression:
		if spanMultiline(elementLines(e)) {
			return true
		}
		for _, a := range e.Arguments {
			if needsBreakAuthor(a.Value) {
				return true
			}
		}
	case *ast.PartialExpression:
		if spanMultiline(elementLines(e)) {
			return true
		}
		for _, a := range e.Arguments {
			if !a.Hole && needsBreakAuthor(a.Value) {
				return true
			}
		}
	}
	return false
}

// renderValue renders e starting at column col, breaking a collection or call onto multiple lines when it (or a nested one) was written multi-line or a single line would exceed fmtMaxWidth. clean mode keeps it flat.
func (f *fmtr) renderValue(e ast.Expression, col int) string {
	if e == nil {
		return ""
	}
	flat := f.expr(e)
	if f.clean {
		return flat
	}
	switch e := e.(type) {
	case *ast.ListLiteral, *ast.DictLiteral, *ast.SetLiteral:
		if needsBreakAuthor(e) || col+len(flat) > fmtMaxWidth {
			return f.renderBrokenCollection(e)
		}
	case *ast.CallExpression:
		if strings.Contains(flat, "\n") { // already multi-line (a broken method chain)
			return flat
		}
		if needsBreakAuthor(e) || col+len(flat) > fmtMaxWidth {
			return f.renderBrokenCall(e, col)
		}
	case *ast.PartialExpression:
		if strings.Contains(flat, "\n") {
			return flat
		}
		if needsBreakAuthor(e) || col+len(flat) > fmtMaxWidth {
			return f.renderBrokenPartial(e, col)
		}
	}
	return flat
}

// renderBrokenCollection renders a list/dict/set literal one element per line (trailing comma), recursing so nested values can break too.
func (f *fmtr) renderBrokenCollection(e ast.Expression) string {
	type item struct {
		prefix string
		value  ast.Expression
	}
	var open, close string
	var items []item
	switch e := e.(type) {
	case *ast.ListLiteral:
		open, close = "[", "]"
		for _, el := range e.Elements {
			items = append(items, item{"", el})
		}
	case *ast.SetLiteral:
		open, close = "{", "}"
		for _, el := range e.Elements {
			items = append(items, item{"", el})
		}
	case *ast.DictLiteral:
		open, close = "{", "}"
		for _, en := range e.Entries {
			if en.Spread {
				items = append(items, item{"...", en.Value})
			} else {
				items = append(items, item{f.expr(en.Key) + ": ", en.Value})
			}
		}
	}
	indent := strings.Repeat("    ", f.depth+1)
	var b strings.Builder
	b.WriteString(open + "\n")
	f.depth++
	for _, it := range items {
		b.WriteString(indent + it.prefix + f.renderValue(it.value, len(indent)+len(it.prefix)) + ",\n")
	}
	f.depth--
	b.WriteString(strings.Repeat("    ", f.depth) + close)
	return b.String()
}

// renderBrokenCall renders a call's arguments one per line; a single collection argument instead hugs the parens (callee([ ... ])).
func (f *fmtr) renderBrokenCall(e *ast.CallExpression, col int) string {
	callee := f.exprChild(e.Callee, !fmtIsPrimary(e.Callee)) + f.typeArgs(e.TypeArguments)
	if len(e.Arguments) == 1 {
		a := e.Arguments[0]
		if !a.Spread && a.Name == nil && isCollection(a.Value) {
			return callee + "(" + f.renderValue(a.Value, col+len(callee)+1) + ")"
		}
	}
	indent := strings.Repeat("    ", f.depth+1)
	var b strings.Builder
	b.WriteString(callee + "(\n")
	f.depth++
	for i, a := range e.Arguments {
		prefix := ""
		if a.Spread {
			prefix = "..."
		} else if a.Name != nil {
			prefix = a.Name.Value + ": "
		}
		b.WriteString(indent + prefix + f.renderValue(a.Value, len(indent)+len(prefix)))
		if i < len(e.Arguments)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	f.depth--
	b.WriteString(strings.Repeat("    ", f.depth) + ")")
	return b.String()
}

func (f *fmtr) callArgs(args []ast.CallArgument) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		s := ""
		if arg.Spread {
			s = "..." + f.expr(arg.Value)
		} else if arg.Name != nil {
			s = arg.Name.Value + ": " + f.expr(arg.Value)
		} else {
			s = f.expr(arg.Value)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

func (f *fmtr) partialArgs(args []ast.CallArgument) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if arg.Hole {
			if arg.Name != nil {
				parts = append(parts, arg.Name.Value+": _")
			} else {
				parts = append(parts, "_")
			}
		} else if arg.Spread {
			parts = append(parts, "..."+f.expr(arg.Value))
		} else if arg.Name != nil {
			parts = append(parts, arg.Name.Value+": "+f.expr(arg.Value))
		} else {
			parts = append(parts, f.expr(arg.Value))
		}
	}
	return strings.Join(parts, ", ")
}

func (f *fmtr) renderBrokenPartial(e *ast.PartialExpression, col int) string {
	callee := f.exprChild(e.Callee, !fmtIsPrimary(e.Callee)) + f.typeArgs(e.TypeArguments)
	if len(e.Arguments) == 1 {
		a := e.Arguments[0]
		if !a.Hole && !a.Spread && a.Name == nil && isCollection(a.Value) {
			return callee + "(" + f.renderValue(a.Value, col+len(callee)+1) + ")"
		}
	}
	indent := strings.Repeat("    ", f.depth+1)
	var b strings.Builder
	b.WriteString(callee + "(\n")
	f.depth++
	for i, a := range e.Arguments {
		if a.Hole {
			prefix := ""
			if a.Name != nil {
				prefix = a.Name.Value + ": "
			}
			b.WriteString(indent + prefix + "_")
		} else {
			prefix := ""
			if a.Spread {
				prefix = "..."
			} else if a.Name != nil {
				prefix = a.Name.Value + ": "
			}
			b.WriteString(indent + prefix + f.renderValue(a.Value, len(indent)+len(prefix)))
		}
		if i < len(e.Arguments)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	f.depth--
	b.WriteString(strings.Repeat("    ", f.depth) + ")")
	return b.String()
}

func (f *fmtr) params(params []ast.Parameter) string {
	parts := make([]string, 0, len(params))
	for _, p := range params {
		parts = append(parts, f.param(p))
	}
	return strings.Join(parts, ", ")
}

// param renders a parameter, using f.expr for the default so dict/list/closure defaults are not emitted as String() placeholders.
func (f *fmtr) param(p ast.Parameter) string {
	prefix := ""
	for _, d := range p.Decorators {
		prefix += f.fmtDecorator(d) + " "
	}
	var seg []string
	if p.Const {
		seg = append(seg, "const")
	}
	if p.Type != nil {
		seg = append(seg, p.Type.String())
	}
	if p.Variadic {
		seg = append(seg, "...")
	}
	if p.Name != nil {
		seg = append(seg, p.Name.Value)
	}
	out := prefix + strings.Join(seg, " ")
	if p.Default != nil {
		out += " = " + f.expr(p.Default)
	}
	return out
}

func (f *fmtr) fmtComprehensionClauses(clauses []ast.ComprehensionClause) string {
	var sb strings.Builder
	for _, clause := range clauses {
		switch c := clause.(type) {
		case *ast.ComprehensionFor:
			sb.WriteString(" for ")
			if c.VarType != nil {
				sb.WriteString(c.VarType.String() + " ")
			}
			if len(c.VarNames) > 0 {
				names := make([]string, 0, len(c.VarNames))
				for _, n := range c.VarNames {
					names = append(names, n.Value)
				}
				sb.WriteString(strings.Join(names, ", "))
			} else if c.VarName != nil {
				sb.WriteString(c.VarName.Value)
			}
			sb.WriteString(" in " + f.expr(c.Iterable))
		case *ast.ComprehensionIf:
			sb.WriteString(" if " + f.expr(c.Filter))
		}
	}
	return sb.String()
}

func (f *fmtr) typeParams(tps []*ast.TypeParam) string {
	parts := make([]string, 0, len(tps))
	for _, tp := range tps {
		s := tp.Name.Value
		if tp.Constraint != nil {
			s += " implements " + tp.Constraint.String()
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}
