package bytecode_test

import (
	"strings"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func TestCompileRejectsUnknownNativeNamedArgument(t *testing.T) {
	for _, src := range []string{
		"import json;\nlet s = json.stringify({\"a\": 1}, indent: 2);\n",
		"import math;\nfunc f(): void { defer math.pow(base: 2.0, zzz: 1.0); }\n",
	} {
		p := parser.New(lexer.New(src))
		program := p.ParseProgram()
		if _, err := bytecode.Compile(program, []byte(src), "native_named"); err == nil || !strings.Contains(err.Error(), "has no parameter") {
			t.Errorf("compile %q: got %v, want a has-no-parameter error", src, err)
		}
	}
}

func TestCompileValidatesNativeArity(t *testing.T) {
	rejected := []string{
		"import math;\nlet x = math.pow(2.0);\n",
		"import math;\nlet x = math.pow(2.0, 3.0, 4.0);\n",
		"import io;\nio.mkdir(\"d\");\n",
		"import secureRandom;\nlet n = secureRandom.randomInt(1, 2, 3, 4);\n",
		"import secureRandom;\nlet n = secureRandom.randomInt(1);\n",
		"import math;\nfunc f(): void { defer math.pow(2.0); }\n",
	}
	for _, src := range rejected {
		p := parser.New(lexer.New(src))
		if _, err := bytecode.Compile(p.ParseProgram(), []byte(src), "native_arity"); err == nil || !strings.Contains(err.Error(), "does not accept") {
			t.Errorf("compile %q: got %v, want an argument-count error", src, err)
		}
	}
	accepted := []string{
		"import math;\nlet x = math.min(3, 1, 4);\n",
		"import path;\nlet p = path.join(\"a\", \"b\", \"c\");\n",
		"import secureRandom;\nlet s = secureRandom.openSession();\nlet n = secureRandom.randomInt(s, 1, 6);\n",
		"import sys;\nlet r = sys.run(\"echo\", \"a\", \"b\");\n",
		"import log;\nlet l = log.stderr();\nlog.info(l, \"m\", {});\n",
		"import ext;\nlet c = ext.load(\"x\");\next.call(c, \"f\", 1, 2, 3);\n",
	}
	for _, src := range accepted {
		p := parser.New(lexer.New(src))
		if _, err := bytecode.Compile(p.ParseProgram(), []byte(src), "native_arity"); err != nil {
			t.Errorf("compile %q: %v", src, err)
		}
	}
}
