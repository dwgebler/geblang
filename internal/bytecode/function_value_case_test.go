package bytecode_test

import (
	"strings"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func TestFunctionValueLookupIsCaseSensitive(t *testing.T) {
	for _, tc := range []struct {
		source string
		err    string
	}{
		{"func helper(): int { return 1; }\nlet f = Helper;", "unknown bytecode name Helper"},
		{"func hidden(): any { return Hidden; }", "unknown bytecode name Hidden"},
		{"func helper(): int { return 1; }\nlet f = helper;", ""},
		{"interface Hidden { func x(): int; }\nfunc hidden(): any { return Hidden; }", ""},
	} {
		p := parser.New(lexer.New(tc.source))
		program := p.ParseProgram()
		if len(p.Errors()) != 0 {
			t.Fatal(p.Errors())
		}
		_, err := bytecode.CompileWithOptions(program, []byte(tc.source), "case", bytecode.CompileOptions{})
		if tc.err == "" {
			if err != nil {
				t.Fatalf("%q: unexpected error %v", tc.source, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Fatalf("%q: error = %v, want %q", tc.source, err, tc.err)
		}
	}
}
